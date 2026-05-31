use async_trait::async_trait;

use crate::{
    application::commands::{ScheduleRequest, ScheduleResponse},
    domain::{NodeAssignment, NodeId},
    error::ControllerError,
    ports::Scheduler,
};

#[derive(Clone)]
pub struct FakeScheduler {
    node_id: NodeId,
}

impl Default for FakeScheduler {
    fn default() -> Self {
        Self { node_id: NodeId("node-dev-1".to_string()) }
    }
}

impl FakeScheduler {
    pub fn new(node_id: impl Into<String>) -> Self {
        Self { node_id: NodeId(node_id.into()) }
    }
}

#[async_trait]
impl Scheduler for FakeScheduler {
    async fn schedule(&self, request: ScheduleRequest) -> Result<ScheduleResponse, ControllerError> {
        Ok(ScheduleResponse {
            assignment: NodeAssignment {
                node_id: self.node_id.clone(),
                reason: format!(
                    "fake scheduler selected {} for build {} with {} MiB",
                    self.node_id.0, request.build.build_id, request.resources.memory_mb
                ),
            },
        })
    }
}
