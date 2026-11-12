//! Checks the manifests and writes the GRN catalogue `lib/grn` commits
//! (ADR-0143 item 2). `make -C policy gen` runs it:
//!
//! ```text
//! cargo run -p server --example manifests -- <catalogue.go> [<schema dir>]
//! ```
//!
//! It exits 1, naming each disagreement and writing nothing, when a
//! manifest breaks a rule every manifest holds, when an `@caller` is
//! neither `none` nor a [`server::Caller`], or when a `Caller` is named
//! by no manifest, or when the schema directory holds a manifest the
//! service does not embed, which nothing would load. The schema directory defaults to `schema`.

use std::path::PathBuf;
use std::process::ExitCode;

use manifest::{Manifests, Writer, embedded, render_go};
use server::Caller;

fn main() -> ExitCode {
    let mut args = std::env::args().skip(1);
    let (Some(output), dir) = (args.next(), args.next()) else {
        eprintln!("manifests: usage: manifests <catalogue.go> [<schema dir>]");
        return ExitCode::from(2);
    };
    let dir = PathBuf::from(dir.unwrap_or_else(|| "schema".to_owned()));
    match run(&PathBuf::from(output), &dir) {
        Ok(()) => ExitCode::SUCCESS,
        Err(problems) => {
            for problem in &problems {
                eprintln!("manifests: {problem}");
            }
            eprintln!("manifests: {} disagreement(s)", problems.len());
            ExitCode::FAILURE
        }
    }
}

fn run(output: &PathBuf, dir: &PathBuf) -> Result<(), Vec<String>> {
    let files = read_sources(dir).map_err(|message| vec![message])?;
    let unloaded: Vec<String> = files
        .iter()
        .filter(|(_, source)| !embedded().contains(&source.as_str()))
        .map(|(path, _)| {
            format!(
                "{}: a manifest the service does not embed; add it to EMBEDDED in crates/manifest/src/manifests.rs",
                path.display()
            )
        })
        .collect();
    if !unloaded.is_empty() {
        return Err(unloaded);
    }
    let sources: Vec<String> = files.into_iter().map(|(_, source)| source).collect();
    let sources: Vec<&str> = sources.iter().map(String::as_str).collect();
    let manifests = Manifests::from_sources(&sources).map_err(|error| {
        let mut chain = vec![error.to_string()];
        let mut source = std::error::Error::source(&error);
        while let Some(cause) = source {
            chain.push(cause.to_string());
            source = cause.source();
        }
        vec![chain.join(": ")]
    })?;

    let mut problems = manifests.problems();
    problems.extend(caller_problems(&manifests));
    if !problems.is_empty() {
        return Err(problems);
    }
    std::fs::write(output, render_go(&manifests))
        .map_err(|error| vec![format!("{}: {error}", output.display())])?;
    println!(
        "manifests: {} services, {} entity types, {} actions; wrote {}",
        manifests.services().len(),
        manifests
            .services()
            .iter()
            .map(|service| service.entity_types.len())
            .sum::<usize>(),
        manifests
            .services()
            .iter()
            .map(|service| service.actions.len())
            .sum::<usize>(),
        output.display()
    );
    Ok(())
}

fn read_sources(dir: &PathBuf) -> Result<Vec<(PathBuf, String)>, String> {
    let mut paths: Vec<PathBuf> = std::fs::read_dir(dir)
        .map_err(|error| format!("{}: {error}", dir.display()))?
        .filter_map(Result::ok)
        .map(|entry| entry.path())
        .filter(|path| path.extension().is_some_and(|ext| ext == "cedarschema"))
        .collect();
    paths.sort();
    if paths.is_empty() {
        return Err(format!("{}: no .cedarschema file", dir.display()));
    }
    paths
        .iter()
        .map(|path| {
            std::fs::read_to_string(path)
                .map(|source| (path.clone(), source))
                .map_err(|error| format!("{}: {error}", path.display()))
        })
        .collect()
}

/// The two rules that need the closed list of callers, which the
/// manifest crate cannot see.
fn caller_problems(manifests: &Manifests) -> Vec<String> {
    let subjects: Vec<&str> = Caller::ALL.iter().map(|caller| caller.subject()).collect();
    let mut problems = Vec::new();
    let mut named = Vec::new();
    for service in manifests.services() {
        for entity in &service.entity_types {
            if let Writer::Caller(sub) = &entity.writer {
                if subjects.contains(&sub.as_str()) {
                    named.push(sub.as_str());
                } else {
                    problems.push(format!(
                        "{}: @caller {sub:?} is neither \"none\" nor a Caller",
                        entity.name
                    ));
                }
            }
        }
    }
    for subject in subjects {
        if !named.contains(&subject) {
            problems.push(format!("Caller {subject:?} is named by no manifest"));
        }
    }
    problems
}
