//! A question asked without permits is decided as a write decides the
//! right to write a `permit` (ADR-0175): by `Evaluate` and by
//! `Evaluations`, against the graph `main` runs.

use super::*;

const KIM_MAY_PERMIT: &str = r#"permit (
    principal == Iam::User::"grn:acme/iam/user/kim",
    action == Iam::Action::"permit.write",
    resource == Iam::Org::"grn:root/iam/org/acme"
);"#;

const PERMIT_WRITE: &str = "iam:permit.write";

impl Platform {
    fn question(&self, who: Named, without_permits: bool) -> EvaluateRequest {
        EvaluateRequest {
            subject: subject(who),
            action: Some(Action {
                name: PERMIT_WRITE.into(),
            }),
            resource: Some(Resource {
                r#type: ORG.0.into(),
                id: ORG.1.into(),
                properties: None,
            }),
            context: None,
            read: 0,
            probe: true,
            without_permits,
        }
    }

    /// Whether `Evaluate` allows `who` `iam:permit.write` on acme.
    async fn evaluate_permit_write(&mut self, who: Named, without_permits: bool) -> bool {
        let request = self.request(self.question(who, without_permits));
        self.grpc
            .evaluate(request)
            .await
            .unwrap()
            .into_inner()
            .decision
    }

    /// The answers of one `Evaluations` call asking each of `who`,
    /// where the request's default is `default` and an entry sets its
    /// own where `own` names one.
    async fn evaluations_permit_write(
        &mut self,
        who: &[(Named, Option<bool>)],
        default: bool,
    ) -> Vec<bool> {
        let template = self.question(ADA, default);
        let request = self.request(pb::EvaluationsRequest {
            subject: None,
            action: template.action,
            resource: template.resource,
            context: None,
            read: 0,
            evaluations: who
                .iter()
                .map(|(who, own)| pb::Evaluation {
                    subject: subject(*who),
                    action: None,
                    resource: None,
                    context: None,
                    probe: None,
                    without_permits: *own,
                })
                .collect(),
            probe: true,
            without_permits: default,
        });
        self.grpc
            .evaluations(request)
            .await
            .unwrap()
            .into_inner()
            .evaluations
            .iter()
            .map(|answer| answer.decision)
            .collect()
    }
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn without_permits_an_owner_is_allowed_and_a_member_refused(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;

    for without_permits in [false, true] {
        assert!(
            service.evaluate_permit_write(OWEN, without_permits).await,
            "owner, without_permits {without_permits}"
        );
        assert!(
            !service.evaluate_permit_write(KIM, without_permits).await,
            "member, without_permits {without_permits}"
        );
    }
    for default in [false, true] {
        assert_eq!(
            service
                .evaluations_permit_write(&[(OWEN, None), (KIM, None)], default)
                .await,
            [true, false],
            "default {default}"
        );
    }
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_permit_naming_permit_write_allows_a_plain_question_and_neither_a_write_nor_the_question_without_permits(
    pool: PgPool,
) {
    seed(&pool).await;
    let mut service = platform(pool).await;

    service
        .create(Some(OWEN), "kim-permits", permit(KIM_MAY_PERMIT))
        .await
        .unwrap();
    assert!(
        !service.evaluate_permit_write(KIM, false).await,
        "a draft decides nothing"
    );
    service.activate(Some(OWEN), "kim-permits").await.unwrap();

    // The control: the permit is stored, active and deciding, so the
    // plain question allows what the refusals below are not for want
    // of the permit.
    assert!(service.evaluate_permit_write(KIM, false).await);
    assert!(!service.evaluate_permit_write(KIM, true).await);
    assert!(service.evaluate_permit_write(OWEN, true).await);

    assert_eq!(
        service
            .evaluations_permit_write(&[(KIM, None), (OWEN, None)], false)
            .await,
        [true, true],
        "plain, by the request"
    );
    assert_eq!(
        service
            .evaluations_permit_write(&[(KIM, None), (OWEN, None)], true)
            .await,
        [false, true],
        "without permits, by the request"
    );
    assert_eq!(
        service
            .evaluations_permit_write(&[(KIM, Some(true)), (KIM, Some(false))], false)
            .await,
        [false, true],
        "an entry replaces the request's"
    );
    assert_eq!(
        service
            .evaluations_permit_write(&[(KIM, Some(true)), (KIM, Some(false))], true)
            .await,
        [false, true],
        "an entry replaces the request's"
    );

    refused(
        service
            .create(Some(KIM), "kim-more", permit(LEE_WATCHES_BOT))
            .await,
        Code::PermissionDenied,
    );
}
