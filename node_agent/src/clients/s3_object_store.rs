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

    async fn object_exists(
        &self,
        bucket: &str,
        key: &str,
    ) -> Result<bool, Box<dyn std::error::Error>> {
        match self.client.head_object().bucket(bucket).key(key).send().await {
            Ok(_) => Ok(true),
            Err(err) => {
                if let Some(service_err) = err.as_service_error() {
                    if matches!(
                        service_err,
                        aws_sdk_s3::operation::head_object::HeadObjectError::NotFound(_)
                    ) {
                        return Ok(false);
                    }
                }
                Err(err.into())
            }
        }
    }

    async fn list_objects(
        &self,
        bucket: &str,
        prefix: &str,
    ) -> Result<Vec<String>, Box<dyn std::error::Error>> {
        let mut keys = Vec::new();
        let mut continuation_token: Option<String> = None;

        loop {
            let mut req = self
                .client
                .list_objects_v2()
                .bucket(bucket)
                .prefix(prefix);
            if let Some(token) = &continuation_token {
                req = req.continuation_token(token);
            }
            let resp = req.send().await?;

            for obj in resp.contents() {
                if let Some(key) = obj.key() {
                    keys.push(key.to_string());
                }
            }

            match resp.is_truncated() {
                Some(true) => {
                    continuation_token = resp.next_continuation_token().map(|t| t.to_string());
                    if continuation_token.is_none() {
                        break;
                    }
                }
                _ => break,
            }
        }

        Ok(keys)
    }

    async fn delete_object(&self, bucket: &str, key: &str) -> Result<(), Box<dyn std::error::Error>> {
        self.client.delete_object().bucket(bucket).key(key).send().await?;
        Ok(())
    }
}
