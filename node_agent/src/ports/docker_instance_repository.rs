use async_trait::async_trait;

use crate::domain::GameContainer;
use crate::error::NodeAgentError;

/// 维护 container_id ↔ GameContainer 映射的持久化层。
///
/// Docker 本身不感知 `GameContainer` 中的 `LocalGameBuild` 等业务字段，
/// 因此需要独立存储来记录每个 Docker 容器对应的完整信息。
#[async_trait]
pub trait DockerInstanceRepository: Send + Sync {
    /// 保存容器记录。
    async fn save(&self, container: &GameContainer) -> Result<(), NodeAgentError>;

    /// 根据 container_id 查询容器记录。
    async fn get(&self, container_id: &str) -> Result<Option<GameContainer>, NodeAgentError>;

    /// 删除容器记录（容器被移除后清理）。
    async fn delete(&self, container_id: &str) -> Result<(), NodeAgentError>;
}
