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

    /// 判断对象是否存在（增量上传短路：已存在则直接引用）
    async fn object_exists(&self, bucket: &str, key: &str) -> Result<bool, Box<dyn std::error::Error>>;

    /// 列出 bucket 下指定前缀的所有对象 key（不含 bucket 前缀，用于 GC）
    async fn list_objects(
        &self,
        bucket: &str,
        prefix: &str,
    ) -> Result<Vec<String>, Box<dyn std::error::Error>>;

    /// 删除单个对象（用于 GC）
    async fn delete_object(&self, bucket: &str, key: &str) -> Result<(), Box<dyn std::error::Error>>;
}
