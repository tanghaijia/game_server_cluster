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

    async fn object_exists(
        &self,
        bucket: &str,
        key: &str,
    ) -> Result<bool, Box<dyn std::error::Error>> {
        let storage_key = format!("{}/{}", bucket, key);
        Ok(self.data.lock().unwrap().contains_key(&storage_key))
    }

    async fn list_objects(
        &self,
        bucket: &str,
        prefix: &str,
    ) -> Result<Vec<String>, Box<dyn std::error::Error>> {
        let bucket_prefix = format!("{}/{}", bucket, prefix);
        let data = self.data.lock().unwrap();
        let mut keys = data
            .keys()
            .filter(|k| k.starts_with(&bucket_prefix))
            .map(|k| k.trim_start_matches(&format!("{}/", bucket)).to_string())
            .collect::<Vec<_>>();
        keys.sort();
        Ok(keys)
    }

    async fn delete_object(&self, bucket: &str, key: &str) -> Result<(), Box<dyn std::error::Error>> {
        let storage_key = format!("{}/{}", bucket, key);
        self.data.lock().unwrap().remove(&storage_key);
        Ok(())
    }
}
