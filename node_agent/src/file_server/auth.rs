use jsonwebtoken::{decode, Algorithm, DecodingKey, Validation};
use serde::{Deserialize, Serialize};

/// 文件/日志会话 token 载荷（controller 用共享 HMAC 密钥签发，见 docs/file-manager-design.md §4.3
/// 与 docs/node-agent-logging-design.md §4.1）
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FileTokenClaims {
    /// 实例级 token 绑定 instance_id；agent 级（agent_logs）签发时为空字符串
    pub instance_id: String,
    pub scope: String,
    pub exp: usize,
}

/// scope：实例文件管理（已有）
pub const TOKEN_SCOPE_FILES: &str = "files";
/// scope：node_agent 自身日志读取（P2，agent 级，不绑实例）
pub const TOKEN_SCOPE_AGENT_LOGS: &str = "agent_logs";

/// 解码并验签 token（HS256），不做 scope/绑定判断；调用方自行决定附加约束。
fn decode_token(token: &str, secret: &[u8]) -> Result<FileTokenClaims, String> {
    let data = decode::<FileTokenClaims>(
        token,
        &DecodingKey::from_secret(secret),
        &Validation::new(Algorithm::HS256),
    )
    .map_err(|e| format!("invalid token: {e}"))?;
    Ok(data.claims)
}

/// 校验文件会话 token：scope=`files` + 绑定 expected_instance
pub fn verify_token(token: &str, secret: &[u8], expected_instance: &str) -> Result<FileTokenClaims, String> {
    let claims = decode_token(token, secret)?;
    if claims.scope != TOKEN_SCOPE_FILES {
        return Err("invalid token scope".to_string());
    }
    if claims.instance_id != expected_instance {
        return Err("token instance mismatch".to_string());
    }
    Ok(claims)
}

/// 校验 agent 日志 token：scope=`agent_logs`（agent 级，不校验实例绑定）
pub fn verify_agent_logs_token(token: &str, secret: &[u8]) -> Result<FileTokenClaims, String> {
    let claims = decode_token(token, secret)?;
    if claims.scope != TOKEN_SCOPE_AGENT_LOGS {
        return Err("invalid token scope".to_string());
    }
    Ok(claims)
}
