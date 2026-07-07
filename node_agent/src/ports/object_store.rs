use async_trait::async_trait;

/// 对象存储抽象：上传/下载二进制数据，按 bucket + key 寻址。
///
/// 生产环境：S3ObjectStore 走 AWS S3
/// 开发环境：InMemoryObjectStore 走内存 HashMap，零网络、零外部依赖
#[async_trait]
pub trait ObjectStore: Send + Sync {
    /// 上传二进制数据
    async fn put_object(
        &self,
        bucket: &str,
        key: &str,
        body: Vec<u8>,
    ) -> Result<(), Box<dyn std::error::Error>>;

    /// 下载二进制数据
    async fn get_object(
        &self,
        bucket: &str,
        key: &str,
    ) -> Result<Vec<u8>, Box<dyn std::error::Error>>;
}
