use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::error::ControllerError;

use super::{
    DesiredState, Endpoint, FailureInfo, GameBuild, GameKind, InstanceId, InstanceSpec,
    NodeAssignment, RuntimeState, SnapshotReference, VersionSelector, ensure_state,
};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GameInstance {
    pub instance_id: InstanceId,
    pub game: GameKind,
    pub desired_state: DesiredState,
    pub runtime_state: RuntimeState,
    pub version_selector: VersionSelector,
    pub resolved_build: Option<GameBuild>,
    pub spec: InstanceSpec,
    pub assignment: Option<NodeAssignment>,
    pub endpoint: Option<Endpoint>,
    pub latest_snapshot: Option<SnapshotReference>,
    pub pending_restore_snapshot: Option<SnapshotReference>,
    pub failure: Option<FailureInfo>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
    pub generation: u64,
}

impl GameInstance {
    pub fn new(
        game: GameKind,
        version_selector: VersionSelector,
        spec: InstanceSpec,
        now: DateTime<Utc>,
    ) -> Self {
        Self {
            instance_id: InstanceId::new(),
            game,
            desired_state: DesiredState::Running,
            runtime_state: RuntimeState::Pending,
            version_selector,
            resolved_build: None,
            spec,
            assignment: None,
            endpoint: None,
            latest_snapshot: None,
            pending_restore_snapshot: None,
            failure: None,
            created_at: now,
            updated_at: now,
            generation: 0,
        }
    }

    pub fn mark_resolving_build(&mut self, now: DateTime<Utc>) -> Result<(), ControllerError> {
        ensure_state(
            &self.instance_id.0,
            &self.runtime_state,
            &[RuntimeState::Pending, RuntimeState::RelocationPending],
            "resolve build",
        )?;
        self.runtime_state = RuntimeState::ResolvingBuild;
        self.touch(now);
        Ok(())
    }

    pub fn mark_scheduling(
        &mut self,
        build: GameBuild,
        now: DateTime<Utc>,
    ) -> Result<(), ControllerError> {
        ensure_state(
            &self.instance_id.0,
            &self.runtime_state,
            &[
                RuntimeState::Pending,
                RuntimeState::RelocationPending,
                RuntimeState::ResolvingBuild,
            ],
            "schedule instance",
        )?;
        self.resolved_build = Some(build);
        self.runtime_state = RuntimeState::Scheduling;
        self.failure = None;
        self.touch(now);
        Ok(())
    }

    pub fn mark_preparing_build(
        &mut self,
        assignment: NodeAssignment,
        now: DateTime<Utc>,
    ) -> Result<(), ControllerError> {
        ensure_state(
            &self.instance_id.0,
            &self.runtime_state,
            &[RuntimeState::Scheduling],
            "prepare build",
        )?;
        self.assignment = Some(assignment);
        self.runtime_state = RuntimeState::PreparingBuild;
        self.failure = None;
        self.touch(now);
        Ok(())
    }

    pub fn mark_starting(&mut self, now: DateTime<Utc>) -> Result<(), ControllerError> {
        ensure_state(
            &self.instance_id.0,
            &self.runtime_state,
            &[RuntimeState::PreparingBuild],
            "start instance",
        )?;
        self.runtime_state = RuntimeState::Starting;
        self.touch(now);
        Ok(())
    }

    pub fn mark_restoring_snapshot(&mut self, now: DateTime<Utc>) -> Result<(), ControllerError> {
        ensure_state(
            &self.instance_id.0,
            &self.runtime_state,
            &[RuntimeState::PreparingBuild],
            "restore snapshot",
        )?;
        self.runtime_state = RuntimeState::RestoringSnapshot;
        self.touch(now);
        Ok(())
    }

    pub fn mark_running(
        &mut self,
        endpoint: Option<Endpoint>,
        now: DateTime<Utc>,
    ) -> Result<(), ControllerError> {
        ensure_state(
            &self.instance_id.0,
            &self.runtime_state,
            &[RuntimeState::Starting, RuntimeState::Running],
            "mark running",
        )?;
        self.runtime_state = RuntimeState::Running;
        self.endpoint = endpoint;
        self.pending_restore_snapshot = None;
        self.failure = None;
        self.touch(now);
        Ok(())
    }

    pub fn request_stop(
        &mut self,
        reason: Option<String>,
        now: DateTime<Utc>,
    ) -> Result<(), ControllerError> {
        ensure_state(
            &self.instance_id.0,
            &self.runtime_state,
            &[RuntimeState::Running, RuntimeState::Starting, RuntimeState::PreparingBuild],
            "request stop",
        )?;
        self.desired_state = DesiredState::Stopped;
        self.runtime_state = RuntimeState::StopRequested;
        self.failure = reason.map(|value| FailureInfo {
            step: "stop-requested".to_string(),
            reason: value,
            retryable: false,
        });
        self.touch(now);
        Ok(())
    }

    pub fn mark_stopping(&mut self, now: DateTime<Utc>) -> Result<(), ControllerError> {
        ensure_state(
            &self.instance_id.0,
            &self.runtime_state,
            &[RuntimeState::StopRequested],
            "stop instance",
        )?;
        self.runtime_state = RuntimeState::Stopping;
        self.touch(now);
        Ok(())
    }

    pub fn mark_stopped(&mut self, now: DateTime<Utc>) -> Result<(), ControllerError> {
        ensure_state(
            &self.instance_id.0,
            &self.runtime_state,
            &[RuntimeState::Stopping, RuntimeState::Stopped],
            "mark stopped",
        )?;
        self.runtime_state = RuntimeState::Stopped;
        self.endpoint = None;
        self.touch(now);
        Ok(())
    }

    pub fn request_relocation(&mut self, now: DateTime<Utc>) -> Result<(), ControllerError> {
        ensure_state(
            &self.instance_id.0,
            &self.runtime_state,
            &[RuntimeState::Stopped],
            "request relocation",
        )?;
        self.desired_state = DesiredState::Running;
        self.runtime_state = RuntimeState::RelocationPending;
        self.assignment = None;
        self.endpoint = None;
        self.failure = None;
        self.touch(now);
        Ok(())
    }

    pub fn request_restore(
        &mut self,
        snapshot: SnapshotReference,
        now: DateTime<Utc>,
    ) -> Result<(), ControllerError> {
        ensure_state(
            &self.instance_id.0,
            &self.runtime_state,
            &[RuntimeState::Stopped],
            "request restore",
        )?;
        self.desired_state = DesiredState::Running;
        self.runtime_state = RuntimeState::RelocationPending;
        self.assignment = None;
        self.endpoint = None;
        self.pending_restore_snapshot = Some(snapshot);
        self.failure = None;
        self.touch(now);
        Ok(())
    }

    pub fn mark_failed(&mut self, failure: FailureInfo, now: DateTime<Utc>) {
        self.runtime_state = RuntimeState::Failed;
        self.failure = Some(failure);
        self.touch(now);
    }

    fn touch(&mut self, now: DateTime<Utc>) {
        self.updated_at = now;
        self.generation += 1;
    }
}
