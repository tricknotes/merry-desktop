//! In-memory backend used by the spike. No VM is touched.

use crate::vm::{ExecResult, Resources, VmBackend, VmError, VmStatus};
use std::sync::Mutex;
use std::thread;
use std::time::Duration;

const START_DELAY: Duration = Duration::from_millis(1500);

pub struct StubBackend {
    state: Mutex<VmStatus>,
    resources: Mutex<Resources>,
}

impl StubBackend {
    pub fn new() -> Self {
        Self {
            state: Mutex::new(VmStatus::Stopped),
            resources: Mutex::new(Resources { cpus: 2, memory_mib: 4096 }),
        }
    }

    fn set_state(&self, status: VmStatus) {
        *self.state.lock().unwrap() = status;
    }
}

impl VmBackend for StubBackend {
    fn start(&self) -> Result<(), VmError> {
        self.set_state(VmStatus::Starting);
        thread::sleep(START_DELAY);
        if std::env::var("SPIKE_STUB_FAIL_START").as_deref() == Ok("1") {
            self.set_state(VmStatus::Error);
            return Err(VmError("stub: start failed (SPIKE_STUB_FAIL_START=1)".into()));
        }
        self.set_state(VmStatus::Running);
        Ok(())
    }

    fn stop(&self) -> Result<(), VmError> {
        self.set_state(VmStatus::Stopped);
        Ok(())
    }

    fn exec(&self, args: &[String]) -> Result<ExecResult, VmError> {
        if *self.state.lock().unwrap() != VmStatus::Running {
            return Err(VmError("stub: VM is not running".into()));
        }
        Ok(ExecResult {
            exit_code: 0,
            stdout: format!("stub: {}", args.join(" ")),
            stderr: String::new(),
        })
    }

    fn status(&self) -> Result<VmStatus, VmError> {
        Ok(*self.state.lock().unwrap())
    }

    fn set_resources(&self, resources: Resources) -> Result<(), VmError> {
        if resources.cpus < 1 {
            return Err(VmError("cpus must be >= 1".into()));
        }
        if resources.memory_mib < 1024 {
            return Err(VmError("memoryMiB must be >= 1024".into()));
        }
        *self.resources.lock().unwrap() = resources;
        Ok(())
    }
}
