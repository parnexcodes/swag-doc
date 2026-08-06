## Purpose

Performance contract for the documentation-generation half of the application: generation must process large captured corpora with work bounded per unique body, must not mutate the data directory it reads, and must keep memory usage bounded by a single session file.

## ADDED Requirements

### Requirement: Generation does not modify the data directory

Running `swagdoc generate` must not create, modify, or delete any file in the data directory it reads from.

#### Scenario: Generate leaves the data directory untouched
- **WHEN** `swagdoc generate` runs against a data directory containing session files
- **THEN** the directory contents are byte-for-byte unchanged after the run

### Requirement: Generation work is bounded per unique body

Each distinct request/response body must be parsed and sanitized at most once per generation run. Corpora containing many occurrences of the same body must not incur repeated full-body processing for those occurrences.

#### Scenario: Repeated bodies do not multiply work
- **WHEN** generation runs on a corpus where the same body shapes recur across many transactions
- **THEN** the measured generation cost is bounded by the number of distinct body shapes plus a linear factor for per-transaction bookkeeping, and a benchmark corpus of repeated bodies completes in a small constant multiple of the time for a corpus of the same distinct bodies (see benchmark suite)

### Requirement: Generation memory is bounded by one file

Generation must process session files one at a time and must not require the entire contents of the data directory to be resident in memory at once.

#### Scenario: Large directories stream one file at a time
- **WHEN** the data directory contains multiple session files
- **THEN** peak memory during generation is bounded by the largest single file plus the in-memory spec structures, independent of the total number of files

### Requirement: Generation completes within a linear bound on corpus size

Generation time must scale approximately linearly with the number of transactions in the corpus; no processing step may exhibit quadratic behavior in the number of transactions, distinct paths, or distinct bodies.

#### Scenario: Doubling the corpus approximately doubles time
- **WHEN** generation runs on a corpus twice the size of a baseline corpus with comparable body shapes
- **THEN** total generation time is at most a small constant multiple of the baseline time (see benchmark suite)
