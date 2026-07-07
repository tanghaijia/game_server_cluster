use async_trait::async_trait;
use std::collections::HashMap;
use std::sync::Mutex;

use crate::ports::ObjectStore;

/// 基于内存 HashMap 的对象存储实现。
///
/// 键格式：`{bucket}/{key}`，所有数据存于 `Vec<u8>`。
/// 零网络、零 I/O，适用于开发/测试环境。
#[derive(Default)]
pub struct InMemoryObjectStore {
    data: Mutex<HashMap<String, Vec<u8>>>,
}

impl InMemoryObjectStore {
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl ObjectStore for InMemoryObjectStore {
    async fn put_object(
        &self,
        bucket: &str,
        key: &str,
        body: Vec<u8>,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let storage_key = format!("{}/{}", bucket, key);
        self.data.lock().unwrap().insert(storage_key, body);
        Ok(())
    }

    async fn get_object(
        &self,
        bucket: &str,
        key: &str,
    ) -> Result<Vec<u8>, Box<dyn std::error::Error>> {
        let storage_key = format!("{}/{}", bucket, key);
        self.data
            .lock()
            .unwrap()
            .get(&storage_key)
            .cloned()
            .ok_or_else(|| format!("object not found: bucket={}, key={}", bucket, key).into())
    }
}
