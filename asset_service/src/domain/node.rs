use serde::{Deserialize, Serialize};

/**
* Node模型
**/
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Node {
    pub id: String,
    pub ip: String,
    pub core_num: i32,
    pub core_frequency: f64,
    pub memory_size: i64,
    pub storage_size: i64,
    pub location: String,
    pub service_provider: String,
    pub status: String
}