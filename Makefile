.PHONY: test

test:
	@go test -v ./... 2>&1 | tee /tmp/.dmn_test_output; \
	passed=$$(grep -c "^--- PASS" /tmp/.dmn_test_output || true); \
	failed=$$(grep -c "^--- FAIL" /tmp/.dmn_test_output || true); \
	echo ""; \
	echo "Results: $$passed passed, $$failed failed"
