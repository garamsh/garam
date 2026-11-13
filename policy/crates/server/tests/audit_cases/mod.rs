//! What the service records of its own writes and denials, and who
//! reads it (ADR-0148), against the graph `main` runs.

use super::*;

impl Platform {
    async fn audit(
        &mut self,
        reader: Option<Named>,
        org: &str,
    ) -> Result<Vec<pb::AuditRecord>, tonic::Status> {
        let request = self.request(pb::ListAuditRecordsRequest {
            writer: reader.and_then(subject),
            org: org.into(),
            page_size: 0,
            page_token: String::new(),
        });
        Ok(self
            .grpc
            .list_audit_records(request)
            .await?
            .into_inner()
            .records)
    }

    /// Whether the engine allows `who` the action on the bot.
    async fn asks(&mut self, who: Named, action: &str, probe: bool) -> bool {
        let request = self.request(EvaluateRequest {
            subject: subject(who),
            action: Some(Action {
                name: action.into(),
            }),
            resource: Some(Resource {
                r#type: BOT.0.into(),
                id: BOT.1.into(),
                properties: None,
            }),
            context: None,
            read: 0,
            probe,
        });
        self.grpc
            .evaluate(request)
            .await
            .unwrap()
            .into_inner()
            .decision
    }
}

fn of_kind(records: Vec<pb::AuditRecord>, kind: pb::AuditRecordKind) -> Vec<pb::AuditRecord> {
    records
        .into_iter()
        .filter(|r| r.kind == i32::from(kind))
        .collect()
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_owner_and_an_administrator_read_the_audit_and_a_member_and_a_foreign_owner_cannot(
    pool: PgPool,
) {
    seed(&pool).await;
    let mut service = platform(pool).await;

    service.audit(Some(OWEN), ACME).await.unwrap();
    service.audit(Some(ADA), ACME).await.unwrap();
    refused(service.audit(Some(LEE), ACME).await, Code::PermissionDenied);
    refused(service.audit(Some(GIL), ACME).await, Code::PermissionDenied);
    refused(service.audit(None, ACME).await, Code::PermissionDenied);
    // Control: the foreign owner reads its own organization's.
    service.audit(Some(GIL), GLOBEX).await.unwrap();

    let denials = of_kind(
        service.audit(Some(OWEN), ACME).await.unwrap(),
        pb::AuditRecordKind::Denial,
    );
    let reads: Vec<_> = denials
        .iter()
        .filter(|r| r.action == "iam:audit.read")
        .map(|r| r.subject.as_str())
        .collect();
    assert!(
        reads.contains(&LEE.1),
        "a denied read is recorded: {reads:?}"
    );
    assert!(reads.contains(&GIL.1), "{reads:?}");
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_denial_is_recorded_naming_its_policies_and_an_allow_or_a_probe_leaves_none(
    pool: PgPool,
) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    let denials = |records| of_kind(records, pb::AuditRecordKind::Denial);

    assert!(service.asks(LEE, "iam:agent.read", false).await);
    assert!(
        denials(service.audit(Some(OWEN), ACME).await.unwrap())
            .iter()
            .all(|r| r.action != "iam:agent.read"),
        "an allow leaves no record"
    );

    service
        .create(Some(OWEN), "nobody", guardrail(NO_ONE_READS_BOT))
        .await
        .unwrap();
    service.activate(Some(OWEN), "nobody").await.unwrap();
    assert!(!service.asks(LEE, "iam:agent.read", true).await);
    assert!(
        denials(service.audit(Some(OWEN), ACME).await.unwrap())
            .iter()
            .all(|r| r.action != "iam:agent.read"),
        "a probe is not recorded"
    );
    assert!(!service.asks(LEE, "iam:agent.read", false).await);
    assert!(!service.asks(LEE, "iam:agent.read", false).await);

    let recorded = denials(service.audit(Some(OWEN), ACME).await.unwrap());
    let [read] = recorded
        .iter()
        .filter(|r| r.action == "iam:agent.read")
        .collect::<Vec<_>>()
        .try_into()
        .unwrap();
    assert_eq!(read.subject, LEE.1);
    assert_eq!(read.resource, BOT.1);
    assert_eq!(read.service, "platform");
    assert_eq!(read.count, 2, "identical denials of an hour collapse");
    assert_eq!(read.policy_ids, [policy_of("nobody")]);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_policy_write_leaves_a_record_naming_who_wrote_it(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;

    service
        .create(Some(OWEN), "watch", permit(LEE_WATCHES_BOT))
        .await
        .unwrap();

    let writes = of_kind(
        service.audit(Some(ADA), ACME).await.unwrap(),
        pb::AuditRecordKind::Write,
    );
    let made = &writes[0];
    assert_eq!(made.resource, policy_of("watch"));
    assert_eq!(made.outcome, "created");
    assert_eq!(made.subject, OWEN.1);
    assert_eq!(made.service, "platform");
    assert_eq!(made.version_to, 1);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_failing_record_changes_no_answer_and_stops_a_write(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool.clone()).await;
    service
        .create(Some(OWEN), "nobody", guardrail(NO_ONE_READS_BOT))
        .await
        .unwrap();
    service.activate(Some(OWEN), "nobody").await.unwrap();
    let before = (
        service.asks(LEE, "iam:agent.read", false).await,
        service.asks(LEE, "iam:agent.watch", false).await,
    );

    sqlx::query("ALTER TABLE audit_records ADD CONSTRAINT refuse CHECK (false) NOT VALID")
        .execute(&pool)
        .await
        .unwrap();

    let after = (
        service.asks(LEE, "iam:agent.read", false).await,
        service.asks(LEE, "iam:agent.watch", false).await,
    );
    assert_eq!(before, after, "a denial is answered as without a record");
    assert_eq!(after, (false, false));
    refused(
        service
            .create(Some(OWEN), "watch", permit(LEE_WATCHES_BOT))
            .await,
        Code::Unavailable,
    );
    assert!(
        service
            .list(Some(OWEN), ACME)
            .await
            .unwrap()
            .iter()
            .all(|p| p.policy != policy_of("watch")),
        "a write that cannot be recorded is not made"
    );
}
