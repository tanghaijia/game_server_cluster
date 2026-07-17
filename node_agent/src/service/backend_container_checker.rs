use std::{sync::Arc, time::Duration};

use tokio::time::sleep;
use tokio_util::sync::CancellationToken;

use crate::domain::{ContainerStatus, GameInstanceStatus};
use crate::ports::{ContainerClient, GameInstanceRepository};

pub struct BackendContainerChecker {
    container_client: Arc<dyn ContainerClient>,
    game_instance_repos: Arc<dyn GameInstanceRepository>,
    token: Option<CancellationToken>,
    handle: Option<tokio::task::JoinHandle<()>>,
}

impl BackendContainerChecker {
    pub fn new(
        container_client: Arc<dyn ContainerClient>,
        game_instance_repos: Arc<dyn GameInstanceRepository>,
    ) -> Self {
        Self {
            container_client,
            game_instance_repos,
            token: None,
            handle: None,
        }
    }

    pub async fn start_check(&mut self) {
        let token = CancellationToken::new();
        let child_token = token.clone();
        self.token = Some(token);

        let client = self.container_client.clone();
        let repos = self.game_instance_repos.clone();
        self.handle = Some(tokio::spawn(async move {
            loop {
                tokio::select! {
                    _ = child_token.cancelled() => {
                        log::info!("[BackendContainerChecker] 后台容器检查任务退出");
                        break;
                    }
                    _ = sleep(Duration::from_secs(1)) => {
                        if let Err(e) = client.update_container_status().await {
                            log::error!("[BackendContainerChecker] 容器状态更新失败 error={:?}", e);
                        };
                        match repos.get_all().await {
                            Ok(instances) => {
                                for instance in &instances {
                                    if let Some(container_id) = &instance.container_id {
                                        match client.get_container(container_id.clone()).await {
                                            Ok(container) => {
                                                let new_status = match container.status {
                                                    ContainerStatus::Running => Some(GameInstanceStatus::Running),
                                                    ContainerStatus::Exited | ContainerStatus::Dead => Some(GameInstanceStatus::Failed),
                                                    _ => None,
                                                };
                                                if let Some(status) = new_status {
                                                    if instance.status != status {
                                                        if let Ok(mut game_instance) = repos.get(instance.id.clone()).await {
                                                            game_instance.status = status;
                                                            if let Err(e) = repos.save(&game_instance).await {
                                                                log::error!("[BackendContainerChecker] 更新实例状态失败 instance={} error={:?}", instance.id, e);
                                                            }
                                                        }
                                                    }
                                                }
                                            }
                                            Err(e) => {
                                                log::warn!("[BackendContainerChecker] 容器检查失败 instance={} error={:?}", instance.id, e);
                                            }
                                        }
                                    }
                                }
                            }
                            Err(e) => {
                                log::error!("[BackendContainerChecker] 获取实例列表失败: {:?}", e);
                            }
                        }
                    }
                }
            }
        }));
    }

    pub async fn stop_check(&mut self) {
        if let Some(token) = &self.token {
            token.cancel();
        }
        // 不 await handle: 后台任务可能卡在 DB 查询中
        // cancel token 后任务会在下次 select! 循环退出
        self.token = None;
        self.handle = None;
    }
}
