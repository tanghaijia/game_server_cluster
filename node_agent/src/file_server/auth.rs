use jsonwebtoken::{decode, Algorithm, DecodingKey, Validation};
use serde::{Deserialize, Serialize};

/// 文件会话 token 载荷（controller 用共享 HMAC 密钥签发，见 docs/file-manager-design.md §4.3）
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FileTokenClaims {
    pub instance_id: String,
    pub scope: String,
    pub exp: usize,
}

pub const TOKEN_SCOPE_FILES: &str = "files";

/// 校验 Bearer token：HS256 验签 + scope + instance 绑定
pub fn verify_token(token: &str, secret: &[u8], expected_instance: &str) -> Result<FileTokenClaims, String> {
    let data = decode::<FileTokenClaims>(
        token,
        &DecodingKey::from_secret(secret),
        &Validation::new(Algorithm::HS256),
    )
    .map_err(|e| format!("invalid token: {e}"))?;

    let claims = data.claims;
    if claims.scope != TOKEN_SCOPE_FILES {
        return Err("invalid token scope".to_string());
    }
    if claims.instance_id != expected_instance {
        return Err("token instance mismatch".to_string());
    }
    Ok(claims)
}
