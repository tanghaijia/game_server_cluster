use serde::{Deserialize, Serialize};

#[derive(Debug, Deserialize, Serialize)]
pub(crate) struct RestoreSnapShotJob {
    game_instance_id: String,
}