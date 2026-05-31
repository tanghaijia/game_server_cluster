mod prediction_in_out_model;
mod predict;
mod service;
mod hard_resource_service;

#[cfg(test)]
mod tests {
    use std::thread::sleep;
    use std::time::{Duration, SystemTime};
    use crate::hard_resource_service::get_current_snapshot;
    use crate::predict::predict_can_start_process;
    use crate::prediction_in_out_model::{ExpectedProcessResource, PredictionConfig, PredictionInputModel};
    use super::*;

    #[test]
    fn it_works() {
        let config = PredictionConfig::default();
        let mut snapshots = vec![get_current_snapshot()];
        let expected_process_resource = ExpectedProcessResource::new(90.0, 200.0);

        let mut i = 0;
        loop {
            snapshots.push(get_current_snapshot());
            sleep(Duration::new(1, 0));
            if i > 9 {
                break;
            }
            i += 1;
        }

        let now = SystemTime::now()
            .duration_since(SystemTime::UNIX_EPOCH)
            .unwrap_or_default()
            .as_millis() as i64;
        let input = PredictionInputModel {
            timestamp_ms: now,
            current: get_current_snapshot(),
            history: snapshots,
            expected: expected_process_resource,
            config,
        };

        let result = predict_can_start_process(&input);
        println!("{:?}", result);
        assert_ne!(result.cpu_ewma, None);
    }

    #[test]
    fn get_current_resource_work() {
        let snapshot = get_current_snapshot();
        assert_ne!(snapshot.cpu_cores, None);
    }
}
