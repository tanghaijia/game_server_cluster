use serde::{Deserialize, Serialize};

/**
* NodeAgent模型
**/
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct NodeAgent {
    pub id: String,
    pub node_id: String,
}