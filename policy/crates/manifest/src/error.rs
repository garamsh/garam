/// Why the manifests do not load or a name is refused.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// A manifest does not parse, or the manifests do not merge.
    #[error("the manifests do not load")]
    Schema(#[source] Box<dyn std::error::Error + Send + Sync>),
    /// A manifest's JSON form is not the shape this reader expects.
    #[error("manifest {0} is not in the form the reader expects")]
    Shape(String),
    /// An action named without its service, or one no manifest
    /// declares.
    #[error("{0} is not an action of a manifest: write <service>:<action>")]
    Action(String),
}
