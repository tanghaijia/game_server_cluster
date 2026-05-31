use async_trait::async_trait;

use crate::{
    domain::{GameBuild, GameKind, VersionSelector},
    error::ControllerError,
};

#[async_trait]
pub trait BuildResolver: Send + Sync {
    async fn resolve_build(
        &self,
        game: &GameKind,
        selector: &VersionSelector,
    ) -> Result<GameBuild, ControllerError>;
}
