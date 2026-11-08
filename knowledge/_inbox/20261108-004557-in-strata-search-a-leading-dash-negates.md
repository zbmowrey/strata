---
type: gotcha
captured: 2026-11-08T00:45:57-05:00
session: 825a4ac1-dbb7-59f6-8fc4-5ea5702044b9
project: _adhoc
sources: []
---
In strata search, a leading dash negates a term: 'claude -p' excludes docs containing 'p'. Quote it as a phrase ("claude -p") to search for the flag.
