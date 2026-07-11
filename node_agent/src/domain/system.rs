use thiserror::Error;

#[derive(Debug, Error)]
pub enum SystemError {
    #[error("get host ip {ip} fail: {message}")]
    HostIPError { ip: String, message: String },
}
