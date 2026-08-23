use std::collections::HashMap;
use std::sync::Arc;

use crate::domain::{
    ConatinerType, ContainerFilePathMappingHost, ContainerStatus, GameContainer, Image,
    ImageRepository, ImageRepositoryCredentials, ImageStatus, LocalGameBuild, MappingPortType,
    RemoteImage,
};
use crate::ports::{ContainerClient, ContainerError, DockerInstanceRepository, ExecOutput};
use async_trait::async_trait;
use bollard::container::LogOutput;
use bollard::exec::{CreateExecOptions, StartExecOptions};
use bollard::models::{ContainerCreateBody, HostConfig, PortBinding};
use bollard::query_parameters::{CreateContainerOptionsBuilder, RemoveContainerOptionsBuilder};
use bollard::Docker;
use chrono::Utc;
use futures_util::StreamExt;
use lmrc_docker::{DockerClient, DockerCredentials};

pub struct DockerContainerClient {
    image_repository: ImageRepository,
    docker_instances: Arc<dyn DockerInstanceRepository>,
}

impl DockerContainerClient {
    pub fn new(
        image_repository: ImageRepository,
        docker_instances: Arc<dyn DockerInstanceRepository>,
    ) -> Self {
        DockerContainerClient {
            image_repository,
            docker_instances,
        }
    }
}

#[async_trait]
impl ContainerClient for DockerContainerClient {
    async fn pull_image(&self, remote_image: &RemoteImage) -> anyhow::Result<Image> {
        let client = DockerClient::new()?;

        let id = Self::map_image_full_name(
            &self.image_repository.address,
            remote_image.name.as_str(),
            remote_image.tag.as_str(),
        );

        // 如果本地已存在，跳过远端 pull
        if client.registry().image_exists_locally(&id).await? {
            println!("镜像本地已存在，跳过 pull: {}", id);
        } else {
            let registry_credentials =
                Self::map_credentials(self.image_repository.image_repository_credentials.clone());
            client
                .images()
                .pull(id.as_str(), Some(registry_credentials))
                .await?;

            println!("私有镜像拉取成功！: {}", remote_image.name);
        }

        Ok(Image {
            id: remote_image.id.clone(),
            name: remote_image.name.clone(),
            tag: remote_image.tag.clone(),
            size: None,
            created_at: Utc::now(),
            status: ImageStatus::Runnable,
        })
    }

    async fn check_image(&self, image: &RemoteImage) -> anyhow::Result<bool> {
        let client = DockerClient::new()?;
        let full_name =
            Self::map_image_full_name(&self.image_repository.address, &image.name, &image.tag);
        Ok(client.registry().image_exists_locally(&full_name).await?)
    }

    async fn last_version(&self, image: &RemoteImage) -> anyhow::Result<String> {
        let client = DockerClient::new()?;
        let images = client.images().list(true).await?;
        let repo_prefix = format!("{}/{}", self.image_repository.address, image.name);

        let mut candidates: Vec<_> = images
            .into_iter()
            .filter(|img| img.repo_tags.iter().any(|t| t.starts_with(&repo_prefix)))
            .collect();
        candidates.sort_by_key(|img| -img.created);

        candidates
            .first()
            .and_then(|img| {
                img.repo_tags
                    .iter()
                    .find(|t| t.starts_with(&repo_prefix))
                    .and_then(|t| t.rsplit(':').next().map(|s| s.to_string()))
            })
            .ok_or_else(|| anyhow::anyhow!("no local image found for {}", image.name))
    }

    async fn get_container(&self, id: String) -> Result<GameContainer, ContainerError> {
        self.docker_instances
            .get(&id)
            .await
            .map_err(|e| ContainerError::Unknown)?
            .ok_or_else(|| ContainerError::NotFound(id))
    }

    async fn create_container(
        &self,
        container_name: String,
        game_build: LocalGameBuild,
        path_mapping: Vec<crate::domain::ContainerFilePathMappingHost>,
        port_mapping: Option<crate::domain::ContainerPortMapping>,
        resource_limitation: Option<crate::domain::ContainerResourceLimitation>,
        env: HashMap<String, String>,
    ) -> Result<GameContainer, ContainerError> {
        let docker = Docker::connect_with_socket_defaults().map_err(bollard_to_io_error)?;

        let image_full_name = Self::map_image_full_name(
            &self.image_repository.address,
            &game_build.image.name,
            &game_build.image.tag,
        );

        // 卷绑定（挂载卷），带权限模式（mapped_permission）
        let mut binds = Vec::new();
        for mapping in &path_mapping {
            binds.push(Self::format_bind_mapping(mapping));
        }

        // 端口映射 + 暴露端口
        let mut exposed_ports: HashMap<String, HashMap<(), ()>> = HashMap::new();
        let mut port_bindings: HashMap<String, Option<Vec<PortBinding>>> = HashMap::new();
        if let Some(ref port_mapping) = port_mapping {
            for port_map in &port_mapping.port_maps {
                let protocol = match port_map.mapping_port_type {
                    MappingPortType::TCP => "tcp",
                    MappingPortType::UDP => "udp",
                };
                let key = format!("{}/{}", port_map.container_port, protocol);
                exposed_ports.insert(key.clone(), HashMap::new());
                port_bindings.insert(
                    key,
                    Some(vec![PortBinding {
                        host_ip: None,
                        host_port: Some(port_map.host_port.to_string()),
                    }]),
                );
            }
        }

        // 容器环境变量（端口注入等：KEY=VALUE 列表）
        let env: Vec<String> = env.into_iter().map(|(k, v)| format!("{k}={v}")).collect();

        // 资源限制（当前 ContainerResourceLimitation 为空结构体，后续扩展）
        let _ = resource_limitation;

        let host_config = HostConfig {
            binds: Some(binds),
            port_bindings: Some(port_bindings),
            ..Default::default()
        };

        let config = ContainerCreateBody {
            image: Some(image_full_name),
            // 以宿主机当前用户的 UID/GID 运行容器进程，解决挂载卷权限问题
            user: current_user_id_gid(),
            env: if env.is_empty() { None } else { Some(env) },
            labels: Some(HashMap::from([(
                "managed-by".to_string(),
                "node-agent".to_string(),
            )])),
            exposed_ports: Some(exposed_ports),
            host_config: Some(host_config),
            ..Default::default()
        };

        let options = Some(
            CreateContainerOptionsBuilder::default()
                .name(&format!("game-{}-{}", game_build.build_id, container_name))
                .build(),
        );

        let response = docker
            .create_container(options, config)
            .await
            .map_err(bollard_to_io_error)?;
        let container_id = response.id;

        // 启动容器；失败则清理并返回错误
        if let Err(err) = docker
            .start_container(
                &container_id,
                None::<bollard::query_parameters::StartContainerOptions>,
            )
            .await
        {
            log::error!("container {} start error, remove it: {}", container_id, err);
            // 容器刚创建、尚未写入 repo，直接通过 Docker 删除做清理
            let _ = docker
                .remove_container(
                    &container_id,
                    Some(
                        RemoveContainerOptionsBuilder::default()
                            .force(true)
                            .build(),
                    ),
                )
                .await;
            return Err(bollard_to_io_error(err));
        }

        println!(
            "[DockerContainerClient] 容器创建并启动成功: {}",
            container_id
        );

        let container = GameContainer {
            id: container_id,
            game_build,
            container: ConatinerType::DockerContainer,
            container_file_path_mapping: path_mapping,
            container_port_mapping: port_mapping,
            resource_limitation,
            status: ContainerStatus::Created,
        };

        // 持久化到 repository
        self.docker_instances
            .save(&container)
            .await
            .map_err(|e| ContainerError::Unknown)?;

        Ok(container)
    }

    async fn stop_container(&self, id: String) -> Result<GameContainer, ContainerError> {
        // 先从 repo 取出记录
        let mut container = self
            .docker_instances
            .get(&id)
            .await
            .map_err(|_| ContainerError::Unknown)?
            .ok_or_else(|| ContainerError::NotFound(id.clone()))?;

        let client = DockerClient::new().map_err(to_io_error)?;
        client
            .containers()
            .get(&id)
            .stop(Some(30))
            .await
            .map_err(|e| match e {
                lmrc_docker::DockerError::ContainerNotFound(_) => ContainerError::NotFound(id),
                _ => ContainerError::Unknown,
            })?;

        container.status = ContainerStatus::Exited;
        self.docker_instances
            .save(&container)
            .await
            .map_err(|_| ContainerError::Unknown)?;

        Ok(container)
    }

    async fn restart_container(&self, id: String) -> Result<GameContainer, ContainerError> {
        // 先从 repo 取出记录
        let mut container = self
            .docker_instances
            .get(&id)
            .await
            .map_err(|_| ContainerError::Unknown)?
            .ok_or_else(|| ContainerError::NotFound(id.clone()))?;

        let client = DockerClient::new().map_err(to_io_error)?;
        client
            .containers()
            .get(&id)
            .restart(Some(30))
            .await
            .map_err(|e| match e {
                lmrc_docker::DockerError::ContainerNotFound(_) => ContainerError::NotFound(id),
                _ => ContainerError::Unknown,
            })?;

        container.status = ContainerStatus::Running;
        self.docker_instances
            .save(&container)
            .await
            .map_err(|_| ContainerError::Unknown)?;

        Ok(container)
    }

    async fn remove_container(&self, id: String) -> Result<GameContainer, ContainerError> {
        let container = self
            .docker_instances
            .get(&id)
            .await
            .map_err(|_| ContainerError::Unknown)?
            .ok_or_else(|| ContainerError::NotFound(id.clone()))?;

        let client = DockerClient::new().map_err(to_io_error)?;
        client
            .containers()
            .get(&id)
            .remove(false, false)
            .await
            .map_err(|e| match e {
                lmrc_docker::DockerError::ContainerNotFound(_) => {
                    ContainerError::NotFound(id.clone())
                }
                _ => ContainerError::Unknown,
            })?;

        // 清除 repo 记录
        self.docker_instances
            .delete(&id)
            .await
            .map_err(|_| ContainerError::Unknown)?;

        Ok(container)
    }

    /**
     * 更新container的状态，使用lmrc获取真实的容器状态并更新到repos中
     */
    async fn update_container_status(&self) -> Result<i32, ContainerError> {
        let client = DockerClient::new().map_err(to_io_error)?;
        let summaries = client.containers().list(true).await.map_err(to_io_error)?;        let mut updated_count = 0i32;

        for summary in &summaries {
            let container_id = match &summary.id {
                Some(id) => id,
                None => continue,
            };

            let docker_state = match &summary.state {
                Some(state) => state,
                None => continue,
            };

            // ContainerSummaryStateEnum → ContainerStatus
            let mapped_status = match format!("{:?}", docker_state).as_str() {
                "CREATED" => ContainerStatus::Created,
                "RUNNING" => ContainerStatus::Running,
                "PAUSED" => ContainerStatus::Paused,
                "RESTARTING" => ContainerStatus::Eestarting,
                "EXITED" => ContainerStatus::Exited,
                "DEAD" => ContainerStatus::Dead,
                "REMOVING" => ContainerStatus::Removing,
                _ => continue,
            };

            // 更新本地 repo 中的记录
            match self.docker_instances.get(container_id).await {
                Ok(Some(mut game_container)) => {
                    game_container.status = mapped_status;
                    if self.docker_instances.save(&game_container).await.is_ok() {
                        updated_count += 1;
                    }
                }
                _ => {
                    // 容器存在于 Docker 但不在本地 repo 中（孤立容器），跳过
                }
            }
        }

        Ok(updated_count)
    }

    async fn exec(&self, container_id: String, cmd: Vec<String>) -> Result<ExecOutput, ContainerError> {
        let docker = Docker::connect_with_socket_defaults().map_err(bollard_to_io_error)?;

        let exec = docker
            .create_exec(
                &container_id,
                CreateExecOptions::<String> {
                    attach_stdout: Some(true),
                    attach_stderr: Some(true),
                    cmd: Some(cmd),
                    ..Default::default()
                },
            )
            .await
            .map_err(bollard_to_io_error)?;

        let mut stdout = String::new();
        let mut stderr = String::new();
        match docker
            .start_exec(&exec.id, None::<StartExecOptions>)
            .await
            .map_err(bollard_to_io_error)?
        {
            bollard::exec::StartExecResults::Attached { mut output, .. } => {
                while let Some(frame) = output.next().await {
                    match frame.map_err(bollard_to_io_error)? {
                        LogOutput::StdOut { message } => {
                            stdout.push_str(&String::from_utf8_lossy(&message))
                        }
                        LogOutput::StdErr { message } => {
                            stderr.push_str(&String::from_utf8_lossy(&message))
                        }
                        _ => {}
                    }
                }
            }
            bollard::exec::StartExecResults::Detached => {
                // detach 模式无输出
            }
        }

        let inspect = docker
            .inspect_exec(&exec.id)
            .await
            .map_err(bollard_to_io_error)?;

        Ok(ExecOutput {
            exit_code: inspect.exit_code.unwrap_or(-1) as i32,
            stdout,
            stderr,
        })
    }

    /// 取容器日志尾部（失败诊断：容器/游戏进程退出时抓取原因）
    async fn container_logs(&self, container_id: String, tail: usize) -> Result<String, ContainerError> {
        use bollard::container::LogsOptions;
        use futures_util::StreamExt;

        let docker = Docker::connect_with_socket_defaults().map_err(bollard_to_io_error)?;
        let options = Some(LogsOptions::<String> {
            stdout: true,
            stderr: true,
            timestamps: true,
            tail: tail.max(1).to_string(),
            ..Default::default()
        });
        let mut stream = docker.logs(&container_id, options);
        let mut out = String::new();
        while let Some(frame) = stream.next().await {
            match frame.map_err(bollard_to_io_error)? {
                LogOutput::StdOut { message } | LogOutput::StdErr { message } => {
                    out.push_str(&String::from_utf8_lossy(&message));
                    if !out.ends_with('\n') {
                        out.push('\n');
                    }
                }
                _ => {}
            }
        }
        Ok(out)
    }
}

impl DockerContainerClient {
    fn map_credentials(
        image_repository_credentials: ImageRepositoryCredentials,
    ) -> DockerCredentials {
        DockerCredentials {
            username: image_repository_credentials.username,
            password: image_repository_credentials.password,
            auth: image_repository_credentials.auth,
            email: image_repository_credentials.email,
            serveraddress: image_repository_credentials.serveraddress,
            identitytoken: image_repository_credentials.identitytoken,
            registrytoken: image_repository_credentials.registrytoken,
        }
    }

    fn map_image_full_name(register_address: &str, image_name: &str, tag: &str) -> String {
        format!("{}/{}:{}", register_address, image_name, tag)
    }

    /// 生成 bind 挂载串 `host:container:mode`。
    ///
    /// `mapped_permission` 映射到 Docker 挂载模式：`"r"` → `ro`，其余（`rw`/`rwx`）→ `rw`。
    fn format_bind_mapping(mapping: &ContainerFilePathMappingHost) -> String {
        let mode = match mapping.mapped_permission.as_str() {
            "r" => "ro",
            _ => "rw",
        };
        format!(
            "{}:{}:{}",
            mapping.host_path.path, mapping.container_file_path.path, mode
        )
    }
}

fn to_io_error(e: lmrc_docker::DockerError) -> ContainerError {
    ContainerError::IOError {
        source: std::io::Error::new(std::io::ErrorKind::Other, e.to_string()),
    }
}

fn bollard_to_io_error(e: bollard::errors::Error) -> ContainerError {
    ContainerError::IOError {
        source: std::io::Error::new(std::io::ErrorKind::Other, e.to_string()),
    }
}

/// 当前宿主机用户的 "UID:GID"。非 unix（如 Windows 开发环境）返回 None，不设置容器 user。
#[cfg(unix)]
fn current_user_id_gid() -> Option<String> {
    // SAFETY: getuid/getgid 是无副作用的 libc 调用
    let uid = unsafe { libc::getuid() };
    let gid = unsafe { libc::getgid() };
    Some(format!("{}:{}", uid, gid))
}

#[cfg(not(unix))]
fn current_user_id_gid() -> Option<String> {
    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::{ContainerFilePath, HostFilePath};

    fn mapping(host: &str, container: &str, permission: &str) -> ContainerFilePathMappingHost {
        ContainerFilePathMappingHost {
            host_path: HostFilePath {
                path: host.to_string(),
            },
            container_file_path: ContainerFilePath {
                path: container.to_string(),
            },
            mapped_permission: permission.to_string(),
        }
    }

    #[test]
    fn test_format_bind_mapping_permissions() {
        // "r" → ro
        assert_eq!(
            DockerContainerClient::format_bind_mapping(&mapping("/data/inst/1", "/server", "r")),
            "/data/inst/1:/server:ro"
        );
        // "rwx" → rw
        assert_eq!(
            DockerContainerClient::format_bind_mapping(&mapping("/data/inst/1", "/data", "rwx")),
            "/data/inst/1:/data:rw"
        );
        // "rw" → rw
        assert_eq!(
            DockerContainerClient::format_bind_mapping(&mapping("/data/inst/1", "/data", "rw")),
            "/data/inst/1:/data:rw"
        );
        // 空 → 默认 rw
        assert_eq!(
            DockerContainerClient::format_bind_mapping(&mapping("/x", "/y", "")),
            "/x:/y:rw"
        );
    }
}