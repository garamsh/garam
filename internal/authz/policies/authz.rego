package gaon.authz

# Default gaon authz policy.
#
# Input shape:
#   input.subject = { "grn": "grn:...", "type": "user", "roles": [...], "policies": [...] }
#   input.object  = { "grn": "grn:...", "type": "doc" }
#   input.action  = "doc:read" / "doc:write" / ...
#
# Default deny. Allow when one of the rules below matches.

default allow := false

# Subject with role "admin" may perform any action.
allow if {
    some role in input.subject.roles
    role == "admin"
}

# Owner relationship (subject GRN equals object owner) grants full access.
allow if {
    owner := data.object_owners[input.object.grn]
    owner == input.subject.grn
}

# Members of "viewer" role may read.
allow if {
    some role in input.subject.roles
    role == "viewer"
    input.action == "doc:read"
}

# Members of "editor" role may read and write.
allow if {
    some role in input.subject.roles
    role == "editor"
    input.action == "doc:read"
}

allow if {
    some role in input.subject.roles
    role == "editor"
    input.action == "doc:write"
}
