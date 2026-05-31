use std::sync::Arc;

use crate::{
    application::commands::{
        CreateInstanceRequest, CreateInstanceResponse, CreateSnapshotRequest,
        CreateSnapshotResponse, ReconcileAction, ReconcileInstanceRequest,
        ReconcileInstanceResponse, RequestStopInstance, RestoreSnapshotRequest,
        RestoreSnapshotResponse, ScheduleRequest,
    },
    domain::{FailureInfo, GameInstance, RuntimeState, SnapshotId, SnapshotReference},
    error::ControllerError,
    ports::{
        BuildResolver, Clock, CompleteSnapshotRecordRequest, CreateSnapshotRecordRequest,
        InstanceRepository, NodeAgentClient, Scheduler, SnapshotService,
    },
};

pub struct ControllerService<R, B, S, N, P, C>
where
    R: InstanceRepository,
    B: BuildResolver,
    S: Scheduler,
    N: NodeAgentClient,
    P: SnapshotService,
    C: Clock,
{
    repository: Arc<R>,
    build_resolver: Arc<B>,
    scheduler: Arc<S>,
    node_agent: Arc<N>,
    snapshots: Arc<P>,
    clock: Arc<C>,
}

impl<R, B, S, N, P, C> ControllerService<R, B, S, N, P, C>
where
    R: InstanceRepository,
    B: BuildResolver,
    S: Scheduler,
    N: NodeAgentClient,
    P: SnapshotService,
    C: Clock,
{
    pub fn new(
        repository: Arc<R>,
        build_resolver: Arc<B>,
        scheduler: Arc<S>,
        node_agent: Arc<N>,
        snapshots: Arc<P>,
        clock: Arc<C>,
    ) -> Self {
        Self {
            repository,
            build_resolver,
            scheduler,
            node_agent,
            snapshots,
            clock,
        }
    }

    pub async fn create_instance(
        &self,
        request: CreateInstanceRequest,
    ) -> Result<CreateInstanceResponse, ControllerError> {
        let instance = GameInstance::new(
            request.game,
            request.version_selector,
            request.spec,
            self.clock.now(),
        );
        self.repository.create(&instance).await?;

        Ok(CreateInstanceResponse { instance })
    }

    pub async fn request_stop(
        &self,
        request: RequestStopInstance,
    ) -> Result<GameInstance, ControllerError> {
        let mut instance = self.load_instance(&request.instance_id.0).await?;
        instance.request_stop(request.reason, self.clock.now())?;
        self.repository.save(&instance).await?;
        Ok(instance)
    }

    pub async fn create_snapshot(
        &self,
        request: CreateSnapshotRequest,
    ) -> Result<CreateSnapshotResponse, ControllerError> {
        let mut instance = self.load_instance(&request.instance_id.0).await?;
        let assignment = instance.assignment.clone().ok_or_else(|| ControllerError::Conflict {
            message: format!(
                "instance {} does not have an assigned node for snapshot creation",
                instance.instance_id.0
            ),
        })?;

        let snapshot_record = self
            .snapshots
            .create_snapshot_record(CreateSnapshotRecordRequest {
                instance_id: instance.instance_id.0.clone(),
                build_id: instance.resolved_build.as_ref().map(|build| build.build_id.clone()),
                snapshot_type: request.snapshot_type,
                source_node: Some(assignment.node_id.0.clone()),
            })
            .await?;

        let snapshot_result = self
            .node_agent
            .create_snapshot(crate::ports::CreateSnapshotRequest {
                node_id: assignment.node_id,
                instance: instance.clone(),
                snapshot_id: snapshot_record.snapshot.snapshot_id.0.clone(),
            })
            .await?;

        let snapshot_record = self
            .snapshots
            .complete_snapshot_record(CompleteSnapshotRecordRequest {
                snapshot_id: snapshot_record.snapshot.snapshot_id.clone(),
                storage_uri: snapshot_result.storage_uri,
                manifest_uri: snapshot_result.manifest_uri,
                checksum: snapshot_result.checksum,
            })
            .await?;

        instance.latest_snapshot = Some(snapshot_record.snapshot.clone());
        self.repository.save(&instance).await?;

        Ok(CreateSnapshotResponse {
            instance,
            snapshot: snapshot_record.snapshot,
        })
    }

    pub async fn restore_snapshot(
        &self,
        request: RestoreSnapshotRequest,
    ) -> Result<RestoreSnapshotResponse, ControllerError> {
        let mut instance = self.load_instance(&request.instance_id.0).await?;
        let restore_plan = self
            .snapshots
            .get_snapshot_restore_plan(&SnapshotId(request.snapshot_id))
            .await?;

        let snapshot = SnapshotReference {
            snapshot_id: restore_plan.snapshot_id,
            storage_uri: Some(restore_plan.storage_uri),
            manifest_uri: restore_plan.manifest_uri,
            checksum: restore_plan.checksum,
        };
        instance.request_restore(snapshot.clone(), self.clock.now())?;
        self.repository.save(&instance).await?;

        Ok(RestoreSnapshotResponse {
            instance,
            snapshot,
        })
    }

    pub async fn reconcile_instance(
        &self,
        request: ReconcileInstanceRequest,
    ) -> Result<ReconcileInstanceResponse, ControllerError> {
        let mut instance = self.load_instance(&request.instance_id.0).await?;
        let now = self.clock.now();

        let last_action = match instance.runtime_state {
            RuntimeState::Pending | RuntimeState::RelocationPending | RuntimeState::ResolvingBuild => {
                if matches!(
                    instance.runtime_state,
                    RuntimeState::Pending | RuntimeState::RelocationPending
                ) {
                    instance.mark_resolving_build(now)?;
                }

                let build = self
                    .build_resolver
                    .resolve_build(&instance.game, &instance.version_selector)
                    .await?;
                instance.mark_scheduling(build.clone(), self.clock.now())?;
                ReconcileAction::ResolvedBuild { build }
            }
            RuntimeState::Scheduling => {
                let build = instance.resolved_build.clone().ok_or_else(|| {
                    ControllerError::Conflict {
                        message: format!(
                            "instance {} entered Scheduling without a resolved build",
                            instance.instance_id.0
                        ),
                    }
                })?;

                let response = self
                    .scheduler
                    .schedule(ScheduleRequest {
                        game: instance.game.clone(),
                        build,
                        resources: instance.spec.resources.clone(),
                    })
                    .await?;
                instance.mark_preparing_build(response.assignment.clone(), self.clock.now())?;
                ReconcileAction::AssignedNode {
                    assignment: response.assignment,
                }
            }
            RuntimeState::PreparingBuild => {
                let assignment = instance.assignment.clone().ok_or_else(|| {
                    ControllerError::Conflict {
                        message: format!(
                            "instance {} entered PreparingBuild without an assignment",
                            instance.instance_id.0
                        ),
                    }
                })?;
                let build = instance.resolved_build.clone().ok_or_else(|| {
                    ControllerError::Conflict {
                        message: format!(
                            "instance {} entered PreparingBuild without a resolved build",
                            instance.instance_id.0
                        ),
                    }
                })?;

                self.node_agent
                    .prepare_game_build(crate::ports::PrepareBuildRequest {
                        node_id: assignment.node_id.clone(),
                        build: build.clone(),
                    })
                    .await?;
                if instance.pending_restore_snapshot.is_some() {
                    instance.mark_restoring_snapshot(self.clock.now())?;
                } else {
                    instance.mark_starting(self.clock.now())?;
                }
                ReconcileAction::BuildPreparationRequested {
                    node_id: assignment.node_id,
                    build,
                }
            }
            RuntimeState::RestoringSnapshot => {
                let assignment = instance.assignment.clone().ok_or_else(|| {
                    ControllerError::Conflict {
                        message: format!(
                            "instance {} entered RestoringSnapshot without an assignment",
                            instance.instance_id.0
                        ),
                    }
                })?;
                let pending_snapshot = instance
                    .pending_restore_snapshot
                    .clone()
                    .ok_or_else(|| ControllerError::Conflict {
                        message: format!(
                            "instance {} entered RestoringSnapshot without a pending snapshot",
                            instance.instance_id.0
                        ),
                    })?;
                let restore_plan = self
                    .snapshots
                    .get_snapshot_restore_plan(&pending_snapshot.snapshot_id)
                    .await?;

                self.node_agent
                    .restore_snapshot(crate::ports::RestoreSnapshotRequest {
                        node_id: assignment.node_id,
                        instance_id: instance.instance_id.clone(),
                        snapshot: restore_plan,
                    })
                    .await?;
                instance.latest_snapshot = Some(pending_snapshot);
                instance.mark_starting(self.clock.now())?;
                ReconcileAction::NoOp
            }
            RuntimeState::Starting => {
                let assignment = instance.assignment.clone().ok_or_else(|| {
                    ControllerError::Conflict {
                        message: format!(
                            "instance {} entered Starting without an assignment",
                            instance.instance_id.0
                        ),
                    }
                })?;

                let response = self
                    .node_agent
                    .start_instance(crate::ports::StartInstanceRequest {
                        node_id: assignment.node_id.clone(),
                        instance: instance.clone(),
                    })
                    .await?;
                instance.mark_running(response.endpoint.clone(), self.clock.now())?;
                ReconcileAction::StartRequested {
                    node_id: assignment.node_id,
                    endpoint: response.endpoint,
                }
            }
            RuntimeState::Running => ReconcileAction::NoOp,
            RuntimeState::StopRequested => {
                let assignment = instance.assignment.clone().ok_or_else(|| {
                    ControllerError::Conflict {
                        message: format!(
                            "instance {} requested stop without an assignment",
                            instance.instance_id.0
                        ),
                    }
                })?;
                instance.mark_stopping(self.clock.now())?;
                self.node_agent
                    .stop_instance(crate::ports::StopInstanceRequest {
                        node_id: assignment.node_id,
                        instance: instance.clone(),
                    })
                    .await?;
                ReconcileAction::StopRequested
            }
            RuntimeState::Stopping => {
                instance.mark_stopped(self.clock.now())?;
                ReconcileAction::MarkedStopped
            }
            RuntimeState::Stopped | RuntimeState::Failed => ReconcileAction::NoOp,
        };

        self.repository.save(&instance).await?;

        Ok(ReconcileInstanceResponse {
            instance,
            last_action,
        })
    }


    pub async fn get_instance(
        &self,
        instance_id: &str,
    ) -> Result<GameInstance, ControllerError> {
        self.load_instance(instance_id).await
    }

    pub async fn list_unfinished_instances(&self) -> Result<Vec<GameInstance>, ControllerError> {
        self.repository.list_unfinished().await
    }

    pub async fn report_runtime_status(
        &self,
        report: crate::application::commands::RuntimeStatusReport,
    ) -> Result<GameInstance, ControllerError> {
        let mut instance = self.load_instance(&report.instance_id.0).await?;
        instance.runtime_state = report.state;
        instance.endpoint = report.endpoint;
        if let Some(message) = report.message {
            instance.failure = Some(crate::domain::FailureInfo {
                step: "runtime-status".to_string(),
                reason: message,
                retryable: true,
            });
        }
        instance.updated_at = self.clock.now();
        instance.generation += 1;
        self.repository.save(&instance).await?;
        Ok(instance)
    }

    pub async fn mark_failed(
        &self,
        instance_id: &str,
        step: &str,
        reason: &str,
        retryable: bool,
    ) -> Result<GameInstance, ControllerError> {
        let mut instance = self.load_instance(instance_id).await?;
        instance.mark_failed(
            FailureInfo {
                step: step.to_string(),
                reason: reason.to_string(),
                retryable,
            },
            self.clock.now(),
        );
        self.repository.save(&instance).await?;
        Ok(instance)
    }

    async fn load_instance(&self, instance_id: &str) -> Result<GameInstance, ControllerError> {
        let id = crate::domain::InstanceId(instance_id.to_string());
        self.repository
            .get(&id)
            .await?
            .ok_or_else(|| ControllerError::InstanceNotFound {
                instance_id: instance_id.to_string(),
            })
    }
}
