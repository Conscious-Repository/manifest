# People lookup outage

Metis logs at 22:44–22:45 Central reported `DeepSeek skipped: endpoint unavailable`. A direct request from Metis to the configured `http://192.168.87.11:8000/v1/models` failed with connection refused in 0.02 seconds. The shared lab service catalog lists that same endpoint. SSH from Metis to both configured Spark hosts timed out; model-service restoration requires a working authorized admin connection or a corrected endpoint from the operator.

Manifest previously labeled every adapter error “unreachable” and combined failures with “nothing new under that exact name.” It also omitted failed sources when another source returned enrichment. The UI now reports incomplete lookups and partial failures separately from successful empty results. DeepSeek errors retain their underlying diagnostic in server logs. No alternate model was substituted, and candidate records were not altered for diagnosis.

Validation: recruiting/source Go suites; browser-message regression for empty, failed, partial and successful lookups; frontend syntax. Infrastructure recovery remains outstanding until the configured endpoint answers and an evidence-backed lookup succeeds.
