use async_trait::async_trait;
use aws_sdk_s3::{Client, primitives::ByteStream};

use crate::ports::ObjectStore;

/// 通过 AWS SDK S3 的真实对象存储实现。
pub struct S3ObjectStore {
    client: Client,
}

impl S3ObjectStore {
    pub fn new(client: Client) -> Self {
        Self { client }
    }
}

#[async_trait]
impl ObjectStore for S3ObjectStore {
    async fn put_object(
        &self,
        bucket: &str,
        key: &str,
        body: Vec<u8>,
    ) -> Result<(), Box<dyn std::error::Error>> {
        self.client
            .put_object()
            .bucket(bucket)
            .key(key)
            .body(ByteStream::from(body))
            .send()
            .await?;
        Ok(())
    }

    async fn get_object(
        &self,
        bucket: &str,
        key: &str,
    ) -> Result<Vec<u8>, Box<dyn std::error::Error>> {
        let resp = self
            .client
            .get_object()
            .bucket(bucket)
            .key(key)
            .send()
            .await?;
        let data = resp.body.collect().await?.into_bytes().to_vec();
        Ok(data)
    }
}
