//! OS-independent VM backend contract.
//!
//! Implementations must not leak backend-specific concepts (WSL distro names,
//! VHDX paths, Lima instance names) through this API.

use serde::{Deserialize, Serialize};
use std::fmt;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum VmStatus {
    Stopped,
    Starting,
    Running,
    Error,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Resources {
    pub cpus: u32,
    #[serde(rename = "memoryMiB")]
    pub memory_mib: u32,
}

#[derive(Clone, Debug)]
#[allow(dead_code)] // exec is part of the contract but unused by the spike UI.
pub struct ExecResult {
    pub exit_code: i32,
    pub stdout: String,
    pub stderr: String,
}

#[derive(Debug)]
pub struct VmError(pub String);

impl fmt::Display for VmError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for VmError {}

pub trait VmBackend: Send + Sync {
    /// Boots the VM and returns once the container engine responds.
    fn start(&self) -> Result<(), VmError>;
    fn stop(&self) -> Result<(), VmError>;
    #[allow(dead_code)]
    fn exec(&self, args: &[String]) -> Result<ExecResult, VmError>;
    #[allow(dead_code)]
    fn status(&self) -> Result<VmStatus, VmError>;
    fn set_resources(&self, resources: Resources) -> Result<(), VmError>;
}
