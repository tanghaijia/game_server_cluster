use std::{sync::Arc, time::Duration};

use tokio::{
    sync::mpsc,
    time::{interval, MissedTickBehavior},
};

use crate::{
    application::commands::ReconcileInstanceRequest,
    domain::{InstanceId, RuntimeState},
    error::ControllerError,
    ports::{BuildResolver, Clock, InstanceRepository, NodeAgentClient, Scheduler, SnapshotService},
    service::ControllerService,
};

#[derive(Clone)]
pub struct ReconcileDispatcher {
    sender: mpsc::Sender<InstanceId>,
}

impl ReconcileDispatcher {
    pub fn new<R, B, S, N, P, C>(
        service: Arc<ControllerService<R, B, S, N, P, C>>,
        queue_capacity: usize,
        scan_interval: Duration,
    ) -> Self
    where
        R: InstanceRepository + 'static,
        B: BuildResolver + 'static,
        S: Scheduler + 'static,
        N: NodeAgentClient + 'static,
        P: SnapshotService + 'static,
        C: Clock + 'static,
    {
        let (sender, mut receiver) = mpsc::channel::<InstanceId>(queue_capacity);
        let worker_sender = sender.clone();
        let worker_service = service.clone();
        tokio::spawn(async move {
            while let Some(instance_id) = receiver.recv().await {
                match worker_service
                    .reconcile_instance(ReconcileInstanceRequest {
                        instance_id: instance_id.clone(),
                    })
                    .await
                {
                    Ok(response) => {
                        if should_continue(&response.instance.runtime_state) {
                            let _ = worker_sender.send(response.instance.instance_id).await;
                        }
                    }
                    Err(error) => {
                        eprintln!(
                            "controller reconcile worker failed for {}: {}",
                            instance_id.0, error
                        );
                    }
                }
            }
        });

        let scanner_sender = sender.clone();
        tokio::spawn(async move {
            let mut ticker = interval(scan_interval);
            ticker.set_missed_tick_behavior(MissedTickBehavior::Delay);
            loop {
                ticker.tick().await;
                match service.list_unfinished_instances().await {
                    Ok(instances) => {
                        for instance in instances {
                            let _ = scanner_sender.send(instance.instance_id).await;
                        }
                    }
                    Err(error) => {
                        eprintln!("controller reconcile scanner failed: {}", error);
                    }
                }
            }
        });

        Self { sender }
    }

    pub async fn enqueue(&self, instance_id: InstanceId) -> Result<(), ControllerError> {
        self.sender
            .send(instance_id)
            .await
            .map_err(|_| ControllerError::DependencyFailure {
                message: "reconcile queue is closed".to_string(),
            })
    }
}

fn should_continue(state: &RuntimeState) -> bool {
    !matches!(state, RuntimeState::Running | RuntimeState::Stopped | RuntimeState::Failed)
}
