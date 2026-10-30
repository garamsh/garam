// A principal reaches only the organizations it belongs to, and no
// policy overrides a forbid (ADR-0137).
forbid (principal, action, resource)
unless { principal in resource.org };
