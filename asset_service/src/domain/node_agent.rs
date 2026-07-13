use serde::{Deserialize, Serialize};

/**
* NodeAgent模型
**/
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct NodeAgent {
    pub node_id: String,
    pub endpoint: String,
    pub status: String,
    pub last_heartbeat_at: i64,
}