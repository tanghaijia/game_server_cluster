use async_trait::async_trait;

use crate::{
    domain::{GameBuild, GameKind, VersionSelector},
    error::ControllerError,
    ports::BuildResolver,
};

#[derive(Default, Clone)]
pub struct FakeBuildResolver;

#[async_trait]
impl BuildResolver for FakeBuildResolver {
    async fn resolve_build(
        &self,
        game: &GameKind,
        selector: &VersionSelector,
    ) -> Result<GameBuild, ControllerError> {
        let (build_id, channel) = match selector {
            VersionSelector::BuildId { build_id } => (build_id.clone(), None),
            VersionSelector::Channel { channel } => {
                let prefix = match game {
                    GameKind::Dst => "dst",
                    GameKind::Minecraft => "minecraft",
                    GameKind::Custom(name) => name,
                };
                (format!("{prefix}-{channel}-demo-build"), Some(channel.clone()))
            }
        };

        Ok(GameBuild {
            build_id,
            game: game.clone(),
            channel,
            adapter_version: Some("adapter-demo-v1".to_string()),
        })
    }
}
