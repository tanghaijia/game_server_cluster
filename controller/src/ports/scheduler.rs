use async_trait::async_trait;

use crate::{
    application::commands::{ScheduleRequest, ScheduleResponse},
    error::ControllerError,
};

#[async_trait]
pub trait Scheduler: Send + Sync {
    async fn schedule(&self, request: ScheduleRequest) -> Result<ScheduleResponse, ControllerError>;
}
