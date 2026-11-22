//! OS-independent core: owns the UI-facing state and drives the backend.

use crate::vm::{Resources, VmBackend, VmError, VmStatus};
use serde::Serialize;
use std::sync::Mutex;

// Changed by the benchmark script to measure incremental backend builds.
pub const BENCH_REVISION: u32 = 0;

#[derive(Clone, Copy, Debug, Serialize)]
pub struct Snapshot {
    pub status: VmStatus,
    pub resources: Resources,
}

type StatusListener = Box<dyn Fn(VmStatus) + Send + Sync>;

pub struct VmController {
    backend: Box<dyn VmBackend>,
    state: Mutex<Snapshot>,
    listener: StatusListener,
}

impl VmController {
    pub fn new(backend: Box<dyn VmBackend>, listener: StatusListener) -> Self {
        Self {
            backend,
            state: Mutex::new(Snapshot {
                status: VmStatus::Stopped,
                resources: Resources { cpus: 2, memory_mib: 4096 },
            }),
            listener,
        }
    }

    pub fn snapshot(&self) -> Snapshot {
        *self.state.lock().unwrap()
    }

    /// Blocks until the backend finishes starting. Call from a worker thread.
    pub fn start(&self) {
        {
            let mut state = self.state.lock().unwrap();
            if matches!(state.status, VmStatus::Starting | VmStatus::Running) {
                return;
            }
            state.status = VmStatus::Starting;
        }
        (self.listener)(VmStatus::Starting);
        let next = match self.backend.start() {
            Ok(()) => VmStatus::Running,
            Err(err) => {
                eprintln!("{err}");
                VmStatus::Error
            }
        };
        self.set_status(next);
    }

    pub fn stop(&self) {
        let next = match self.backend.stop() {
            Ok(()) => VmStatus::Stopped,
            Err(err) => {
                eprintln!("{err}");
                VmStatus::Error
            }
        };
        self.set_status(next);
    }

    pub fn set_resources(&self, resources: Resources) -> Result<(), VmError> {
        self.backend.set_resources(resources)?;
        self.state.lock().unwrap().resources = resources;
        Ok(())
    }

    fn set_status(&self, status: VmStatus) {
        self.state.lock().unwrap().status = status;
        (self.listener)(status);
    }
}
