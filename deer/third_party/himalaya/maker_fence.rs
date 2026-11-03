//! Persistent producer authority, checked by the final matching execution owner.

use std::collections::BTreeMap;
use std::fs::{File, OpenOptions};
use std::io::{self, Write};
use std::path::PathBuf;
use std::sync::{Mutex, MutexGuard};

use serde::{Deserialize, Serialize};

const MAX_SCOPES: usize = 1024;
const MIN_LEASE_MS: u64 = 2_000;
const MAX_LEASE_MS: u64 = 30_000;

#[derive(Clone, Copy, Debug, Deserialize, Eq, Ord, PartialEq, PartialOrd, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum Kind {
    Execution,
    Strategy,
}

#[derive(Clone, Debug, Deserialize, Eq, Ord, PartialEq, PartialOrd, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Scope {
    pub kind: Kind,
    pub user_id: i64,
    pub fund_id: i64,
    pub symbol: String,
}

impl Scope {
    pub fn valid(&self) -> bool {
        self.user_id > 0
            && self.fund_id > 0
            && match self.kind {
                Kind::Execution => self.symbol.is_empty(),
                Kind::Strategy => {
                    !self.symbol.is_empty()
                        && self.symbol.len() <= 32
                        && self
                            .symbol
                            .bytes()
                            .all(|c| c.is_ascii_uppercase() || c.is_ascii_digit())
                }
            }
    }
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Proof {
    pub scope: Scope,
    pub owner: String,
    pub epoch: u64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Lease {
    pub proof: Proof,
    /// Ticks of the current matching process's monotonic authority clock.
    pub expires_at: u64,
}

/// Captured at admission by the matching process, never from a caller's clock.
#[derive(Clone, Debug)]
pub struct Authorization {
    pub execution: Proof,
    pub strategy: Proof,
    pub deadline: u64,
}

/// Kept until the matching effect is complete; a new grant cannot race the write.
pub struct EffectGuard<'a> {
    _owner: MutexGuard<'a, State>,
}

#[derive(Debug, Eq, PartialEq)]
pub enum Error {
    Invalid,
    Held,
    Lost,
    Capacity,
    Exhausted,
    Durability,
}

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
struct Checkpoint {
    version: u32,
    entries: Vec<Proof>,
}

#[derive(Default)]
struct State {
    leases: BTreeMap<Scope, Lease>,
    failed: bool,
}

pub struct Registry {
    state: Mutex<State>,
    path: Option<PathBuf>,
    _file_owner: Option<File>,
}

fn valid_owner(owner: &str) -> bool {
    !owner.is_empty()
        && owner.len() <= 128
        && owner
            .bytes()
            .all(|c| c.is_ascii_alphanumeric() || b"._-:".contains(&c))
}

impl Registry {
    pub fn from_env() -> io::Result<Self> {
        Self::load(
            std::env::var_os("PERSIST_OUTBOX_DIR")
                .map(|directory| PathBuf::from(directory).join("matching-maker-authorities.json")),
        )
    }

    pub fn load(path: Option<PathBuf>) -> io::Result<Self> {
        let mut state = State::default();
        let mut owner = None;
        if let Some(path) = &path {
            let parent = path
                .parent()
                .ok_or_else(|| io::Error::other("missing authority directory"))?;
            std::fs::create_dir_all(parent)?;
            let file = OpenOptions::new()
                .create(true)
                .truncate(false)
                .read(true)
                .write(true)
                .open(path.with_extension("lock"))?;
            file.try_lock().map_err(io::Error::other)?;
            owner = Some(file);
            let bytes = match std::fs::read(path) {
                Ok(bytes) => Some(bytes),
                Err(error) if error.kind() == io::ErrorKind::NotFound => None,
                Err(error) => return Err(error),
            };
            if let Some(bytes) = bytes {
                let checkpoint: Checkpoint =
                    serde_json::from_slice(&bytes).map_err(io::Error::other)?;
                if checkpoint.version != 1 || checkpoint.entries.len() > MAX_SCOPES {
                    return Err(io::Error::other("invalid producer authority checkpoint"));
                }
                for proof in checkpoint.entries {
                    if !proof.scope.valid() || !valid_owner(&proof.owner) || proof.epoch == 0 {
                        return Err(io::Error::other("invalid producer authority record"));
                    }
                    let lease = Lease {
                        proof,
                        expires_at: 0,
                    };
                    if state
                        .leases
                        .insert(lease.proof.scope.clone(), lease)
                        .is_some()
                    {
                        return Err(io::Error::other("duplicate producer authority scope"));
                    }
                }
            }
        }
        Ok(Self {
            state: Mutex::new(state),
            path,
            _file_owner: owner,
        })
    }

    pub fn durable(&self) -> bool {
        self.path.is_some()
    }

    pub fn claim(
        &self,
        scope: Scope,
        owner: &str,
        ttl: u64,
        clock: impl FnOnce() -> u64,
    ) -> Result<Lease, Error> {
        if !scope.valid() || !valid_owner(owner) || !(MIN_LEASE_MS..=MAX_LEASE_MS).contains(&ttl) {
            return Err(Error::Invalid);
        }
        let mut state = self.state.lock().map_err(|_| Error::Durability)?;
        let now = clock();
        let expires_at = now.checked_add(ttl).ok_or(Error::Invalid)?;
        if state.failed {
            return Err(Error::Durability);
        }
        let epoch = match state.leases.get(&scope) {
            Some(current) if current.expires_at > now => {
                return if current.proof.owner == owner {
                    Ok(current.clone())
                } else {
                    Err(Error::Held)
                };
            }
            Some(current) => current.proof.epoch.checked_add(1).ok_or(Error::Exhausted)?,
            None => {
                if state.leases.len() == MAX_SCOPES {
                    return Err(Error::Capacity);
                }
                1
            }
        };
        let lease = Lease {
            proof: Proof {
                scope: scope.clone(),
                owner: owner.into(),
                epoch,
            },
            expires_at,
        };
        let mut next = state.leases.clone();
        next.insert(scope, lease.clone());
        self.commit(&mut state, next)?;
        Ok(lease)
    }

    pub fn renew(
        &self,
        proof: &Proof,
        ttl: u64,
        clock: impl FnOnce() -> u64,
    ) -> Result<Lease, Error> {
        if !(MIN_LEASE_MS..=MAX_LEASE_MS).contains(&ttl) {
            return Err(Error::Invalid);
        }
        let mut state = self.state.lock().map_err(|_| Error::Durability)?;
        let now = clock();
        let expires_at = now.checked_add(ttl).ok_or(Error::Invalid)?;
        let mut lease = Self::authorized(&state, proof, now)?;
        lease.expires_at = expires_at;
        let mut next = state.leases.clone();
        next.insert(proof.scope.clone(), lease.clone());
        self.commit(&mut state, next)?;
        Ok(lease)
    }

    pub fn authorize(&self, proof: &Proof, clock: impl FnOnce() -> u64) -> Result<Lease, Error> {
        let state = self.state.lock().map_err(|_| Error::Durability)?;
        Self::authorized(&state, proof, clock())
    }

    pub fn release(&self, proof: &Proof, clock: impl FnOnce() -> u64) -> Result<(), Error> {
        let mut state = self.state.lock().map_err(|_| Error::Durability)?;
        let mut lease = Self::authorized(&state, proof, clock())?;
        lease.expires_at = 0;
        let mut next = state.leases.clone();
        next.insert(proof.scope.clone(), lease);
        self.commit(&mut state, next)
    }

    pub fn admit(
        &self,
        execution: Proof,
        strategy: Proof,
        max_age: u64,
        clock: impl FnOnce() -> u64,
    ) -> Result<Authorization, Error> {
        if max_age == 0 || max_age > 2_000 {
            return Err(Error::Invalid);
        }
        let state = self.state.lock().map_err(|_| Error::Durability)?;
        let now = clock();
        let executor = Self::authorized(&state, &execution, now)?;
        let controller = Self::authorized(&state, &strategy, now)?;
        if execution.scope.kind != Kind::Execution
            || strategy.scope.kind != Kind::Strategy
            || execution.scope.user_id != strategy.scope.user_id
            || execution.scope.fund_id != strategy.scope.fund_id
        {
            return Err(Error::Invalid);
        }
        let deadline = now
            .checked_add(max_age)
            .ok_or(Error::Invalid)?
            .min(executor.expires_at)
            .min(controller.expires_at);
        Ok(Authorization {
            execution,
            strategy,
            deadline,
        })
    }

    pub fn effect<'a>(
        &'a self,
        user_id: i64,
        fund_id: i64,
        symbol: &str,
        authorization: &Authorization,
        clock: impl FnOnce() -> u64,
    ) -> Result<EffectGuard<'a>, Error> {
        let state = self.state.lock().map_err(|_| Error::Durability)?;
        let now = clock();
        if authorization.execution.scope
            != (Scope {
                kind: Kind::Execution,
                user_id,
                fund_id,
                symbol: String::new(),
            })
            || authorization.strategy.scope
                != (Scope {
                    kind: Kind::Strategy,
                    user_id,
                    fund_id,
                    symbol: symbol.into(),
                })
        {
            return Err(Error::Invalid);
        }
        Self::authorized(&state, &authorization.execution, now)?;
        Self::authorized(&state, &authorization.strategy, now)?;
        if now >= authorization.deadline {
            return Err(Error::Lost);
        }
        Ok(EffectGuard { _owner: state })
    }

    fn authorized(state: &State, proof: &Proof, now: u64) -> Result<Lease, Error> {
        if state.failed {
            return Err(Error::Durability);
        }
        if !proof.scope.valid() || !valid_owner(&proof.owner) || proof.epoch == 0 {
            return Err(Error::Invalid);
        }
        state
            .leases
            .get(&proof.scope)
            .filter(|current| current.proof == *proof && current.expires_at > now)
            .cloned()
            .ok_or(Error::Lost)
    }

    fn commit(&self, state: &mut State, next: BTreeMap<Scope, Lease>) -> Result<(), Error> {
        if let Some(path) = &self.path {
            let result = (|| {
                let checkpoint = Checkpoint {
                    version: 1,
                    entries: next.values().map(|lease| lease.proof.clone()).collect(),
                };
                let bytes = serde_json::to_vec(&checkpoint).map_err(io::Error::other)?;
                let temporary = path.with_extension("tmp");
                let mut file = OpenOptions::new()
                    .create(true)
                    .truncate(true)
                    .write(true)
                    .open(&temporary)?;
                file.write_all(&bytes)?;
                file.sync_all()?;
                std::fs::rename(&temporary, path)?;
                File::open(
                    path.parent()
                        .ok_or_else(|| io::Error::other("missing authority directory"))?,
                )?
                .sync_all()
            })();
            if result.is_err() {
                // Even rename may have completed. No caller may keep using an
                // old in-memory owner after an ambiguous durable fence write.
                state.failed = true;
                return Err(Error::Durability);
            }
        }
        state.leases = next;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU64, Ordering};
    static NEXT: AtomicU64 = AtomicU64::new(1);

    fn scope(kind: Kind) -> Scope {
        Scope {
            kind,
            user_id: 10001,
            fund_id: 10001,
            symbol: if kind == Kind::Strategy {
                "BTCUSDT".into()
            } else {
                String::new()
            },
        }
    }

    #[test]
    fn expiry_takeover_fences_the_paused_previous_owner() {
        let registry = Registry::load(None).unwrap();
        let first = registry
            .claim(scope(Kind::Execution), "execution.first", 2_000, || 10)
            .unwrap();
        assert_eq!(
            registry.claim(scope(Kind::Execution), "execution.second", 2_000, || 11),
            Err(Error::Held)
        );
        assert!(registry.authorize(&first.proof, || 2_009).is_ok());
        assert_eq!(registry.authorize(&first.proof, || 2_010), Err(Error::Lost));
        let second = registry
            .claim(scope(Kind::Execution), "execution.second", 2_000, || 2_010)
            .unwrap();
        assert!(second.proof.epoch > first.proof.epoch);
        assert_eq!(registry.authorize(&first.proof, || 2_011), Err(Error::Lost));
        assert_eq!(
            registry.renew(&first.proof, 2_000, || 2_011),
            Err(Error::Lost)
        );
        assert!(registry.authorize(&second.proof, || 2_011).is_ok());
    }

    #[test]
    fn scoped_authorities_and_renewal_do_not_grant_each_other() {
        let registry = Registry::load(None).unwrap();
        let execution = registry
            .claim(scope(Kind::Execution), "exec", 2_000, || 0)
            .unwrap();
        let strategy = registry
            .claim(scope(Kind::Strategy), "strat", 2_000, || 0)
            .unwrap();
        let renewed = registry.renew(&execution.proof, 3_000, || 1_000).unwrap();
        assert_eq!(renewed.proof, execution.proof);
        assert_eq!(renewed.expires_at, 4_000);
        assert_eq!(
            registry.authorize(&strategy.proof, || 2_000),
            Err(Error::Lost)
        );
        let mut forged = execution.proof.clone();
        forged.scope = scope(Kind::Strategy);
        assert_eq!(registry.authorize(&forged, || 1_001), Err(Error::Lost));
        assert_eq!(
            registry.claim(scope(Kind::Execution), "", 2_000, || 0),
            Err(Error::Invalid)
        );
        assert_eq!(
            registry.renew(&execution.proof, u64::MAX, || 1_001),
            Err(Error::Invalid)
        );
    }

    #[test]
    fn restart_retains_epochs_but_invalidates_all_previous_process_leases() {
        let directory = std::env::temp_dir().join(format!(
            "muskdeer-fence-{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        ));
        std::fs::create_dir_all(&directory).unwrap();
        let path = directory.join("fences.json");
        let first = Registry::load(Some(path.clone())).unwrap();
        assert!(first.durable());
        let lease = first
            .claim(scope(Kind::Execution), "old", 2_000, || 100)
            .unwrap();
        assert!(Registry::load(Some(path.clone())).is_err());
        drop(first);
        let recovered = Registry::load(Some(path.clone())).unwrap();
        assert_eq!(recovered.authorize(&lease.proof, || 101), Err(Error::Lost));
        let current = recovered
            .claim(scope(Kind::Execution), "new", 2_000, || 101)
            .unwrap();
        assert!(current.proof.epoch > lease.proof.epoch);
        drop(recovered);
        std::fs::write(&path, b"truncated").unwrap();
        assert!(Registry::load(Some(path)).is_err());
        std::fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn ambiguous_checkpoint_failure_closes_authority_instead_of_using_old_memory() {
        let directory = std::env::temp_dir().join(format!(
            "muskdeer-fence-{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        ));
        std::fs::create_dir_all(&directory).unwrap();
        let path = directory.join("fences.json");
        let registry = Registry::load(Some(path.clone())).unwrap();
        let lease = registry
            .claim(scope(Kind::Execution), "old", 2_000, || 0)
            .unwrap();
        std::fs::remove_file(&path).unwrap();
        std::fs::create_dir(&path).unwrap();
        assert_eq!(
            registry.renew(&lease.proof, 2_000, || 1),
            Err(Error::Durability)
        );
        assert_eq!(
            registry.authorize(&lease.proof, || 2),
            Err(Error::Durability)
        );
        drop(registry);
        std::fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn already_queued_command_cannot_cross_a_successful_owner_handover() {
        let registry = Registry::load(None).unwrap();
        let execution = registry
            .claim(scope(Kind::Execution), "exec.old", 2_000, || 0)
            .unwrap();
        let strategy = registry
            .claim(scope(Kind::Strategy), "strat", 2_000, || 0)
            .unwrap();
        let queued = registry
            .admit(execution.proof.clone(), strategy.proof.clone(), 500, || 1)
            .unwrap();
        registry.release(&execution.proof, || 2).unwrap();
        let successor = registry
            .claim(scope(Kind::Execution), "exec.new", 2_000, || 3)
            .unwrap();
        assert!(matches!(
            registry.effect(10001, 10001, "BTCUSDT", &queued, || 4),
            Err(Error::Lost)
        ));
        let fresh = registry
            .admit(successor.proof, strategy.proof, 500, || 4)
            .unwrap();
        assert!(
            registry
                .effect(10001, 10001, "BTCUSDT", &fresh, || 5)
                .is_ok()
        );
        assert!(matches!(
            registry.effect(10001, 10001, "ETHUSDT", &fresh, || 5),
            Err(Error::Invalid)
        ));
        assert!(matches!(
            registry.effect(10002, 10002, "BTCUSDT", &fresh, || 5),
            Err(Error::Invalid)
        ));
        assert!(matches!(
            registry.effect(10001, 10001, "BTCUSDT", &fresh, || 504),
            Err(Error::Lost)
        ));
    }

    #[test]
    fn grant_acknowledgement_waits_until_the_authorized_effect_releases_ownership() {
        let registry = std::sync::Arc::new(Registry::load(None).unwrap());
        let execution = registry
            .claim(scope(Kind::Execution), "exec.old", 2_000, || 0)
            .unwrap();
        let strategy = registry
            .claim(scope(Kind::Strategy), "strat", 2_000, || 0)
            .unwrap();
        let admitted = registry
            .admit(execution.proof.clone(), strategy.proof, 500, || 1)
            .unwrap();
        let effect = registry
            .effect(10001, 10001, "BTCUSDT", &admitted, || 2)
            .unwrap();
        assert!(registry.state.try_lock().is_err());
        let other = registry.clone();
        let (done, result) = std::sync::mpsc::channel();
        let successor = std::thread::spawn(move || {
            done.send(other.claim(scope(Kind::Execution), "exec.new", 2_000, || 2_001))
                .unwrap();
        });
        drop(effect);
        let granted = result
            .recv_timeout(std::time::Duration::from_secs(1))
            .unwrap()
            .unwrap();
        successor.join().unwrap();
        assert!(granted.proof.epoch > execution.proof.epoch);
        assert!(matches!(
            registry.effect(10001, 10001, "BTCUSDT", &admitted, || 3),
            Err(Error::Lost)
        ));
    }
}
