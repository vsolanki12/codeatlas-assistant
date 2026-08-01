# HyperShift Engineering Conventions

## Feature Gates
- New API fields MUST be gated behind a FeatureGate
- Use TechPreviewNoUpgrade for new features, Default for GA
- Gate YAML lives in api/hypershift/v1beta1/featuregates/
- CRD manifests are generated per-gate in zz_generated.featuregated-crd-manifests/
- Each gate has a corresponding featuregated CRD manifest YAML (often 8000+ lines)
- Gate names are PascalCase: HCPUserFacingOperatorLogs, ClusterVersionOperatorConfiguration

## API Design
- Use string enum types (type MyEnum string + const block) for user-facing config, not raw int/bool
- LogLevel follows operatorv1.LogLevel pattern: Normal, Debug, Trace, TraceAll
- Non-pointer value types with omitempty for "no opinion" semantics
- Per-component operator specs: KubeAPIServerOperatorSpec, EtcdOperatorSpec, etc.
- OperatorConfiguration groups all component specs under HostedClusterSpec
- Deep copy methods are auto-generated in zz_generated.deepcopy.go
- Apply configurations go in client/applyconfiguration/hypershift/v1beta1/

## Components (8 total)
When a feature affects "control plane components", it typically covers all 8:
1. kube-apiserver (KAS)
2. etcd
3. kube-controller-manager (KCM)
4. kube-scheduler
5. openshift-controller-manager (OCM)
6. openshift-apiserver (OAPI)
7. openshift-oauth-apiserver
8. oauth-server

## Code Patterns
- (int, bool) return pattern for optional flags — only inject --v when bool is true
- Preserve existing defaults when feature is unset (don't hardcode --v=2 everywhere)
- Deprecation: new OperatorConfiguration field takes precedence over legacy annotations
- Legacy annotation example: hypershift.openshift.io/kube-api-server-verbose
- Reconciler functions: reconcileKubeAPIServer, reconcileEtcd, etc. in control-plane-operator/
- Use ctrl.LoggerFrom(ctx) for structured logging in controllers
- Wrap errors with fmt.Errorf("context: %w", err)
- Use apierrors.IsNotFound(err) for k8s API error checks

## Testing
- Golden fixture tests: testdata/<component>/<variant>/zz_fixture_*.yaml
- One fixture directory per component per configuration variant
- CEL envtest YAML suites for CRD field validation
- Unit tests for resolve/conversion functions (LogLevelToKlogVerbosity, LogLevelToEtcdLevel)
- Run: make verify, make test-envtest-ocp
- Test file naming: <package>_test.go in same directory

## Directory Structure
- API types: api/hypershift/v1beta1/
- CRD manifests: api/hypershift/v1beta1/zz_generated.featuregated-crd-manifests/
- HostedCluster controller: hypershift-operator/controllers/hostedcluster/
- HostedControlPlane controller: control-plane-operator/controllers/hostedcontrolplane/
- Component reconcilers: control-plane-operator/controllers/hostedcontrolplane/v2/
- Support utilities: support/util/
- CLI commands: cmd/cluster/core/
- Client apply configs: client/applyconfiguration/hypershift/v1beta1/

## PR Conventions
- Title format: JIRA-KEY: short description
- Must include unit tests + golden fixture tests for new features
- make verify must pass before merge
- Feature-gated changes need both Default and TechPreviewNoUpgrade gate YAMLs
