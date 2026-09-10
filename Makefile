.PHONY: test up-d

up-d:
	docker compose up -d

test:
	@go test -v ./... 2>&1 | tee dmn_test_output; \
	passed=$$(grep -c "^--- PASS" dmn_test_output || true); \
	failed=$$(grep -c "^--- FAIL" dmn_test_output || true); \
	echo ""; \
	echo "Results: $$passed passed, $$failed failed"
