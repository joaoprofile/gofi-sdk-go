# Stopword lists

`<language>.stop` are the stopword lists PostgreSQL ships in
`share/tsearch_data` (copied from PostgreSQL 16), which come from the Snowball
project (https://snowballstem.org, BSD-3-Clause). The MySQL dialect drops these
words from searches on an index of that language, so both databases ignore the
same words. Do not edit: refresh them from a PostgreSQL release instead.
