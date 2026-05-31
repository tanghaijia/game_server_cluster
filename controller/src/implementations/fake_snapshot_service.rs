use std::{collections::HashMap, sync::{Arc, Mutex}};

use async_trait::async_trait;

use crate::{
    domain::{instance_data_path, SnapshotId, SnapshotReference},
    error::ControllerError,
    ports::{
        CompleteSnapshotRecordRequest, CreateSnapshotRecordRequest, SnapshotRecord,
        SnapshotRestorePlan, SnapshotService,
    },
};

#[derive(Default, Clone)]
pub struct FakeSnapshotService {
    records: Arc<Mutex<HashMap<String, SnapshotRecord>>>,
}

#[async_trait]
impl SnapshotService for FakeSnapshotService {
    async fn create_snapshot_record(
        &self,
        request: CreateSnapshotRecordRequest,
    ) -> Result<SnapshotRecord, ControllerError> {
        let snapshot_id = SnapshotId(format!("snap-{}", request.instance_id));
        let record = SnapshotRecord {
            snapshot: SnapshotReference {
                snapshot_id: snapshot_id.clone(),
                storage_uri: None,
                manifest_uri: None,
                checksum: None,
            },
            build_id: request.build_id,
            instance_data_path: instance_data_path(&request.instance_id),
        };
        let mut records = self.records.lock().map_err(|_| ControllerError::DependencyFailure {
            message: "snapshot service lock poisoned".to_string(),
        })?;
        records.insert(snapshot_id.0.clone(), record.clone());
        Ok(record)
    }

    async fn complete_snapshot_record(
        &self,
        request: CompleteSnapshotRecordRequest,
    ) -> Result<SnapshotRecord, ControllerError> {
        let mut records = self.records.lock().map_err(|_| ControllerError::DependencyFailure {
            message: "snapshot service lock poisoned".to_string(),
        })?;
        let record = records.get_mut(&request.snapshot_id.0).ok_or_else(|| ControllerError::DependencyFailure {
            message: format!("snapshot {} not found", request.snapshot_id.0),
        })?;
        record.snapshot.storage_uri = Some(request.storage_uri);
        record.snapshot.manifest_uri = request.manifest_uri;
        record.snapshot.checksum = request.checksum;
        Ok(record.clone())
    }

    async fn get_snapshot_restore_plan(
        &self,
        snapshot_id: &SnapshotId,
    ) -> Result<SnapshotRestorePlan, ControllerError> {
        let records = self.records.lock().map_err(|_| ControllerError::DependencyFailure {
            message: "snapshot service lock poisoned".to_string(),
        })?;
        let record = records.get(&snapshot_id.0).ok_or_else(|| ControllerError::DependencyFailure {
            message: format!("snapshot {} not found", snapshot_id.0),
        })?;
        Ok(SnapshotRestorePlan {
            snapshot_id: record.snapshot.snapshot_id.clone(),
            build_id: record.build_id.clone(),
            storage_uri: record.snapshot.storage_uri.clone().unwrap_or_else(|| format!("memory://snapshots/{}.tar.zst", snapshot_id.0)),
            manifest_uri: record.snapshot.manifest_uri.clone(),
            checksum: record.snapshot.checksum.clone(),
            instance_data_path: record.instance_data_path.clone(),
        })
    }
}
