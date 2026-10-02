# Urd Cluster Example — Kind-based Kubernetes deployment
#
# Usage:
#   make all       — Create Kind cluster, build, deploy, and wait until it is ready
#   make teardown  — Delete the Kind cluster
#
# Prerequisites: docker, kind, kubectl, curl

.PHONY: all kind-create docker-build deploy wait load-test \
        db reset status logs teardown clean \
        dashboard grafana jaeger prometheus

CLUSTER   := urd-cluster
IMAGE     := urd-cluster-example:latest
NAMESPACE := urd-example
APP_LABEL := app=urd-cluster
BASE_URL  := http://localhost

# NGINX Ingress Controller manifest for Kind
INGRESS_MANIFEST := https://kind.sigs.k8s.io/examples/ingress/deploy-ingress-nginx.yaml

# ---------------------------------------------------------------------------
# Composite targets
# ---------------------------------------------------------------------------

all: kind-create docker-build deploy wait
	@echo ""
	@echo "=== All steps completed successfully ==="
	@echo ""
	@echo "  The Urd API is available at $(BASE_URL)"
	@echo "  Requests are load-balanced across 3 pods via NGINX Ingress."
	@echo ""
	@echo "Next steps:"
	@echo ""
	@echo "  Observability UIs (each blocks; press Ctrl+C to stop):"
	@echo "    make grafana     — Grafana dashboard   http://localhost:3000  (admin/admin)"
	@echo "    make jaeger      — Jaeger trace UI     http://localhost:16686"
	@echo "    make prometheus  — Prometheus query UI http://localhost:9090"
	@echo "    make dashboard   — Kubernetes dashboard https://localhost:8443"
	@echo ""
	@echo "  Testing & load:"
	@echo "    make load-test   — create 1000 accounts (20 concurrent) and report throughput"
	@echo ""
	@echo "  Inspect:"
	@echo "    make db          — snapshot PostgreSQL tables (events, offsets, balances)"
	@echo "    make status      — show all Kubernetes resources in namespace $(NAMESPACE)"
	@echo "    make logs        — tail logs from all app pods"
	@echo "    make reset       — truncate all tables for a clean re-run"
	@echo ""
	@echo "  Cleanup:"
	@echo "    make teardown    — delete the Kind cluster and all resources"

# ---------------------------------------------------------------------------
# Kind cluster (with ingress port mappings)
# ---------------------------------------------------------------------------

kind-create:
	@echo "==> Creating Kind cluster '$(CLUSTER)' with ingress support..."
	@kind get clusters 2>/dev/null | grep -q '^$(CLUSTER)$$' || \
		kind create cluster --name $(CLUSTER) --config kind-config.yaml
	@kubectl cluster-info --context kind-$(CLUSTER)

# ---------------------------------------------------------------------------
# Docker build & load
# ---------------------------------------------------------------------------

docker-build:
	@echo "==> Building Docker image '$(IMAGE)'..."
	@# Build from the repo root so the replace directive (../../) in go.mod resolves.
	docker build -t $(IMAGE) -f Dockerfile ../..
	@echo "==> Loading image into Kind..."
	kind load docker-image $(IMAGE) --name $(CLUSTER)

# ---------------------------------------------------------------------------
# Kubernetes deploy
# ---------------------------------------------------------------------------

deploy:
	@echo "==> Installing NGINX Ingress Controller..."
	kubectl apply --context kind-$(CLUSTER) -f $(INGRESS_MANIFEST)
	@echo "==> Waiting for ingress controller to be ready..."
	kubectl wait --context kind-$(CLUSTER) -n ingress-nginx \
		--for=condition=ready pod -l app.kubernetes.io/component=controller --timeout=120s
	@echo "==> Deploying to namespace '$(NAMESPACE)'..."
	kubectl apply --context kind-$(CLUSTER) -f k8s/namespace.yaml
	kubectl apply --context kind-$(CLUSTER) -f k8s/postgres.yaml
	kubectl apply --context kind-$(CLUSTER) -f k8s/rbac.yaml
	@echo "==> Deploying observability stack..."
	kubectl apply --context kind-$(CLUSTER) -f k8s/grafana-dashboard.yaml
	kubectl apply --context kind-$(CLUSTER) -f k8s/observability.yaml
	@echo "==> Waiting for PostgreSQL to be ready..."
	kubectl wait --context kind-$(CLUSTER) -n $(NAMESPACE) \
		--for=condition=ready pod -l app=postgres --timeout=120s
	@echo "==> Waiting for observability stack to be ready..."
	kubectl wait --context kind-$(CLUSTER) -n $(NAMESPACE) \
		--for=condition=ready pod -l app=otel-collector --timeout=120s
	kubectl apply --context kind-$(CLUSTER) -f k8s/app.yaml

# ---------------------------------------------------------------------------
# Wait for app pods + ingress
# ---------------------------------------------------------------------------

wait:
	@echo "==> Waiting for StatefulSet to roll out (pods start sequentially)..."
	kubectl rollout status --context kind-$(CLUSTER) -n $(NAMESPACE) \
		statefulset/urd-cluster --timeout=300s
	@echo "==> All pods are ready."
	kubectl get pods --context kind-$(CLUSTER) -n $(NAMESPACE)
	@echo ""
	@echo "==> Waiting for ingress to be routable..."
	@READY=0; for i in $$(seq 1 30); do \
		if curl -sf -o /dev/null $(BASE_URL)/healthz 2>/dev/null; then \
			READY=$$((READY + 1)); \
			if [ "$$READY" -ge 3 ]; then \
				echo "    Ingress is ready ($$READY consecutive checks passed)."; \
				break; \
			fi; \
		else \
			READY=0; \
		fi; \
		echo "    Waiting... ($$i/30)"; \
		sleep 2; \
	done

# ---------------------------------------------------------------------------
# Load test — create 1000 accounts, 50 concurrent
# ---------------------------------------------------------------------------

load-test:
	@echo ""
	@echo "==> Load test: creating 1000 accounts (sequential)..."
	@echo ""
	@TMPDIR=$$(mktemp -d); \
	TOTAL=1000; \
	START=$$(date +%s); \
	for j in $$(seq 1 $$TOTAL); do \
		curl -s -o /dev/null \
			-D "$$TMPDIR/hdr-$$j" \
			-w "%{http_code}" \
			-X POST "$(BASE_URL)/accounts/$$(printf 'acct-%04d' $$j)" \
			-H 'Content-Type: application/json' \
			-d '{"balance":1000}' > "$$TMPDIR/http-$$j"; \
		[ $$((j % 100)) -eq 0 ] && printf "    Progress: %d/%d\r" "$$j" "$$TOTAL" || true; \
	done; \
	echo ""; \
	ELAPSED=$$(($$(date +%s) - START)); \
	PASS=$$(grep -l "^2" $$TMPDIR/http-* 2>/dev/null | wc -l | tr -d ' '); \
	FAIL=$$((TOTAL - PASS)); \
	[ $$ELAPSED -gt 0 ] && RPS=$$((TOTAL / ELAPSED)) || RPS=$$TOTAL; \
	echo "    Accounts:   $$TOTAL"; \
	echo "    Pass:       $$PASS"; \
	echo "    Fail:       $$FAIL"; \
	echo "    Duration:   $${ELAPSED}s"; \
	echo "    Throughput: ~$${RPS} accounts/s"; \
	echo ""; \
	echo "    Pod distribution (X-Served-By):"; \
	grep -h -i "x-served-by:" $$TMPDIR/hdr-* 2>/dev/null \
		| awk '{print $$2}' | tr -d '\r' | sort | uniq -c | sort -rn \
		| while read count pod; do \
			printf "      %-50s %4d requests\n" "$$pod" "$$count"; \
		done; \
	echo ""; \
	rm -rf "$$TMPDIR"; \
	if [ "$$FAIL" -gt 50 ]; then \
		echo "=== FAIL: too many failures ($$FAIL/$$TOTAL) — check pod logs ==="; exit 1; \
	elif [ "$$FAIL" -gt 0 ]; then \
		echo "=== WARN: $$FAIL failure(s) — may include duplicates if run twice ==="; \
	else \
		echo "=== PASS: all $$TOTAL accounts created successfully ==="; \
	fi
	@echo ""

# ---------------------------------------------------------------------------
# Observability UIs (port-forward to localhost)
# ---------------------------------------------------------------------------

grafana:
	@echo "==> Grafana: http://localhost:3000  (admin / admin)"
	@echo "    Dashboard: http://localhost:3000/d/urd-cluster-dashboard"
	@echo "    Press Ctrl+C to stop."
	kubectl port-forward --context kind-$(CLUSTER) -n $(NAMESPACE) svc/grafana 3000:3000

jaeger:
	@echo "==> Jaeger UI: http://localhost:16686"
	@echo "    Press Ctrl+C to stop."
	kubectl port-forward --context kind-$(CLUSTER) -n $(NAMESPACE) svc/jaeger 16686:16686

prometheus:
	@echo "==> Prometheus: http://localhost:9090"
	@echo "    Press Ctrl+C to stop."
	kubectl port-forward --context kind-$(CLUSTER) -n $(NAMESPACE) svc/prometheus 9090:9090

# ---------------------------------------------------------------------------
# Kubernetes dashboard
# ---------------------------------------------------------------------------

dashboard:
	@echo "==> Installing Kubernetes dashboard..."
	@kubectl apply --context kind-$(CLUSTER) \
		-f https://raw.githubusercontent.com/kubernetes/dashboard/v2.7.0/aio/deploy/recommended.yaml 2>/dev/null || true
	@echo "==> Creating dashboard admin user..."
	kubectl apply --context kind-$(CLUSTER) -f k8s/dashboard-admin.yaml
	@echo "==> Waiting for dashboard pod to be ready..."
	kubectl wait --context kind-$(CLUSTER) -n kubernetes-dashboard \
		--for=condition=ready pod -l k8s-app=kubernetes-dashboard --timeout=120s
	@echo ""
	@echo "==> Dashboard token (copy this):"
	@kubectl --context kind-$(CLUSTER) -n kubernetes-dashboard create token admin-user 2>/dev/null || \
		kubectl --context kind-$(CLUSTER) -n kubernetes-dashboard get secret \
			$$(kubectl --context kind-$(CLUSTER) -n kubernetes-dashboard get sa admin-user -o jsonpath='{.secrets[0].name}') \
			-o jsonpath='{.data.token}' | base64 -d
	@echo ""
	@echo ""
	@echo ""
	@echo "==> Kubernetes Dashboard: https://localhost:8443"
	@echo ""
	@echo "    IMPORTANT: You must use https:// (not http://)."
	@echo "    Accept the self-signed certificate warning in your browser."
	@echo "    Press Ctrl+C to stop port-forwarding."
	@echo ""
	kubectl port-forward --context kind-$(CLUSTER) -n kubernetes-dashboard deployment/kubernetes-dashboard 8443:8443 || true

# ---------------------------------------------------------------------------
# General helpers
# ---------------------------------------------------------------------------

db:
	@echo "==> Database snapshot (urd / urd-example)"
	@echo ""
	@PSQL="kubectl exec --context kind-$(CLUSTER) -n $(NAMESPACE) statefulset/postgres -- \
		psql -U urd -d urd -c"; \
	echo "--- account_balances (projection read model) ---"; \
	eval $$PSQL '"\x auto \\ SELECT account_id, balance, to_timestamp(updated_at/1000.0) AS updated_at FROM account_balances ORDER BY account_id LIMIT 50;"'; \
	echo ""; \
	echo "--- events_store (last 20 events) ---"; \
	eval $$PSQL '"SELECT persistence_id, sequence_number, event_manifest, to_timestamp(timestamp/1000.0) AS ts FROM events_store ORDER BY timestamp DESC LIMIT 20;"'; \
	echo ""; \
	echo "--- offsets_store (projection progress) ---"; \
	eval $$PSQL '"SELECT projection_name, shard_number, current_offset, to_timestamp(timestamp/1000.0) AS ts FROM offsets_store ORDER BY projection_name, shard_number;"'; \
	echo ""; \
	echo "--- row counts ---"; \
	eval $$PSQL '"SELECT '\''account_balances'\'' AS table_name, COUNT(*) FROM account_balances UNION ALL SELECT '\''events_store'\'', COUNT(*) FROM events_store UNION ALL SELECT '\''offsets_store'\'', COUNT(*) FROM offsets_store ORDER BY 1;"'

reset:
	@echo "==> Truncating all Urd tables for a clean re-run..."
	@kubectl exec --context kind-$(CLUSTER) -n $(NAMESPACE) statefulset/postgres -- \
		psql -U urd -d urd -c \
		"TRUNCATE events_store, offsets_store, account_balances;"
	@echo "    Done. Tables are empty — ready for make load-test."

status:
	kubectl get all --context kind-$(CLUSTER) -n $(NAMESPACE)

logs:
	kubectl logs --context kind-$(CLUSTER) -n $(NAMESPACE) -l $(APP_LABEL) --tail=100 -f

# ---------------------------------------------------------------------------
# Cleanup
# ---------------------------------------------------------------------------

teardown:
	@echo "==> Deleting Kind cluster '$(CLUSTER)'..."
	kind delete cluster --name $(CLUSTER)

clean: teardown
