use async_trait::async_trait;

use crate::{
    domain::{BuildPreparation, BuildPreparationResult},
    error::NodeAgentError,
};

#[async_trait]
pub trait BuildRuntime: Send + Sync {
    async fn prepare_build(
        &self,
        request: BuildPreparation,
    ) -> Result<BuildPreparationResult, NodeAgentError>;
}
