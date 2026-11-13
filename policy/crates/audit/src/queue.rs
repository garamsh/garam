//! The bounded queue a denial waits in for its record, and the counts
//! the recorder keeps.

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Mutex, PoisonError};
use std::time::Duration;

use tokio::sync::{mpsc, oneshot};
use tokio::task::JoinHandle;

use crate::denial::{Denial, Noted, Store};

/// What the recorder counts, in process, since start. The service
/// serves it on its health listener.
#[derive(Debug, Default)]
pub(crate) struct Counters {
    pub(crate) lost: AtomicU64,
    pub(crate) omitted: AtomicU64,
    pub(crate) overflowed: AtomicU64,
    queue_full: AtomicU64,
    queued: AtomicU64,
}

impl Counters {
    pub(crate) fn snapshot(&self) -> Counts {
        Counts {
            lost: self.lost.load(Ordering::Relaxed),
            omitted: self.omitted.load(Ordering::Relaxed),
            overflowed: self.overflowed.load(Ordering::Relaxed),
            queue_full: self.queue_full.load(Ordering::Relaxed),
            queued: self.queued.load(Ordering::Relaxed),
        }
    }
}

/// The recorder's counts as of one moment.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Counts {
    /// Denials not recorded because the record failed or passed its
    /// bound, the queue was full, or the service stopped with them
    /// queued.
    pub lost: u64,
    /// Denials left unrecorded because no organization of the store's
    /// is the resource's or a field was too long.
    pub omitted: u64,
    /// Denials not stored because their organization was at its cap,
    /// and counted in its day's overflow record.
    pub overflowed: u64,
    /// Denials offered to a full queue: a part of `lost`.
    pub queue_full: u64,
    /// Denials waiting for their record now.
    pub queued: u64,
}

/// A denial the queue owns until it is recorded.
#[derive(Debug)]
struct Entry {
    service: String,
    subject: String,
    action: String,
    resource: String,
    policies: Vec<String>,
}

impl Entry {
    fn of(denial: &Denial<'_>) -> Self {
        Self {
            service: denial.service.to_owned(),
            subject: denial.subject.to_owned(),
            action: denial.action.to_owned(),
            resource: denial.resource.to_owned(),
            policies: denial.policies.to_vec(),
        }
    }

    fn as_denial(&self) -> Denial<'_> {
        Denial {
            service: &self.service,
            subject: &self.subject,
            action: &self.action,
            resource: &self.resource,
            policies: &self.policies,
        }
    }
}

#[derive(Debug)]
enum Message {
    Denial(Entry),
    /// Answered once every denial before it has been recorded.
    Settled(oneshot::Sender<()>),
}

/// A bounded channel and the one worker that records what it carries,
/// a denial at a time, in the order they were noted.
#[derive(Debug)]
pub(crate) struct Queue {
    sender: mpsc::Sender<Message>,
    worker: Mutex<Option<JoinHandle<()>>>,
}

impl Queue {
    pub(crate) fn start(store: Store, capacity: usize) -> Self {
        let (sender, receiver) = mpsc::channel(capacity);
        Self {
            sender,
            worker: Mutex::new(Some(tokio::spawn(work(store, receiver)))),
        }
    }

    pub(crate) fn offer(&self, counters: &Counters, denial: &Denial<'_>) -> Noted {
        // Counted before it is sent, so the worker cannot take it off
        // the count first.
        counters.queued.fetch_add(1, Ordering::Relaxed);
        match self.sender.try_send(Message::Denial(Entry::of(denial))) {
            Ok(()) => Noted::Queued,
            Err(error) => {
                counters.queued.fetch_sub(1, Ordering::Relaxed);
                counters.lost.fetch_add(1, Ordering::Relaxed);
                if matches!(error, mpsc::error::TrySendError::Full(_)) {
                    counters.queue_full.fetch_add(1, Ordering::Relaxed);
                    tracing::error!("a denial is not recorded: the queue is full");
                } else {
                    tracing::error!("a denial is not recorded: the queue has stopped");
                }
                Noted::Lost
            }
        }
    }

    pub(crate) async fn settled(&self, within: Duration) -> bool {
        let (done, settled) = oneshot::channel();
        let waiting = async {
            self.sender.send(Message::Settled(done)).await.is_ok() && settled.await.is_ok()
        };
        tokio::time::timeout(within, waiting).await == Ok(true)
    }

    pub(crate) async fn finish(&self, counters: &Counters, within: Duration) -> u64 {
        if self.settled(within).await {
            return 0;
        }
        let worker = self
            .worker
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .take();
        if let Some(worker) = worker {
            worker.abort();
            // The worker has stopped before what it left is counted,
            // so the denial it held is counted once.
            let _ = worker.await;
        }
        let abandoned = counters.queued.swap(0, Ordering::Relaxed);
        counters.lost.fetch_add(abandoned, Ordering::Relaxed);
        if abandoned > 0 {
            tracing::error!(
                abandoned,
                "denials still queued at shutdown are not recorded"
            );
        }
        abandoned
    }
}

async fn work(store: Store, mut receiver: mpsc::Receiver<Message>) {
    while let Some(message) = receiver.recv().await {
        match message {
            Message::Denial(entry) => {
                store.deny(&entry.as_denial()).await;
                store.counters.queued.fetch_sub(1, Ordering::Relaxed);
            }
            Message::Settled(done) => {
                // The asker may have given up waiting.
                let _ = done.send(());
            }
        }
    }
}
