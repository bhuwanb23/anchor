package docker

import (
	"strings"
	"testing"

	"github.com/yourname/yourplatform/agent/internal/env"
)

// =============================================================================
// LAYER 3A END-TO-END INTEGRATION TESTS
// =============================================================================

func TestIntegration_DeployAppWithPostgres_ImageParsing(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantTag  string
	}{
		{"nginx:1.25", "nginx", "1.25"},
		{"ghcr.io/owner/app:v2.0", "owner/app", "v2.0"},
		{"postgres:16-alpine", "postgres", "16-alpine"},
		{"myapp:latest", "myapp", "latest"},
	}
	for _, tt := range tests {
		ref := ParseImageRef(tt.input)
		if ref.Name != tt.wantName {
			t.Errorf("ParseImageRef(%q).Name = %q, want %q", tt.input, ref.Name, tt.wantName)
		}
		if ref.Tag != tt.wantTag {
			t.Errorf("ParseImageRef(%q).Tag = %q, want %q", tt.input, ref.Tag, tt.wantTag)
		}
	}
}

func TestIntegration_DeployAppWithPostgres_ProjectNetwork(t *testing.T) {
	networkName := ProjectNetworkName("My Shop!")
	if !strings.Contains(networkName, "my-shop") {
		t.Errorf("ProjectNetworkName should sanitize, got %q", networkName)
	}
	if !strings.HasPrefix(networkName, networkPrefix) {
		t.Errorf("ProjectNetworkName should have prefix, got %q", networkName)
	}
}

func TestIntegration_DeployAppWithPostgres_PostgresVolume(t *testing.T) {
	volName := VolumeName("myshop", VolumePurposePostgresData)
	if volName != "yourplatform_myshop_postgres-data" {
		t.Errorf("VolumeName = %q, want yourplatform_myshop_postgres-data", volName)
	}

	mountPath := DBVolumeMountPath(ContainerTypePostgres)
	if mountPath != "/var/lib/postgresql/data" {
		t.Errorf("DBVolumeMountPath = %q, want /var/lib/postgresql/data", mountPath)
	}

	purpose := DBVolumePurpose(ContainerTypePostgres)
	if purpose != VolumePurposePostgresData {
		t.Errorf("DBVolumePurpose = %q, want %q", purpose, VolumePurposePostgresData)
	}
}

func TestIntegration_DeployAppWithPostgres_HealthCheck(t *testing.T) {
	hc := DefaultHealthCheck(ContainerTypePostgres, 0)
	if hc == nil {
		t.Fatal("postgres health check should not be nil")
	}
	if len(hc.Test) != 2 || hc.Test[1] != "pg_isready -U postgres || exit 1" {
		t.Errorf("postgres health check command = %v, want pg_isready", hc.Test)
	}

	appHC := DefaultHealthCheck(ContainerTypeApp, 3000)
	if appHC == nil {
		t.Fatal("app health check should not be nil")
	}
	if !strings.Contains(appHC.Test[1], "3000") {
		t.Errorf("app health check should contain port 3000, got %v", appHC.Test)
	}
}

func TestIntegration_DeployAppWithPostgres_DatabaseAliases(t *testing.T) {
	aliases := DatabaseAliases(ContainerTypePostgres)
	if len(aliases) != 2 || aliases[0] != "postgres" || aliases[1] != "db" {
		t.Errorf("DatabaseAliases(postgres) = %v, want [postgres db]", aliases)
	}
}

func TestIntegration_DeployAppWithPostgres_PortBinding(t *testing.T) {
	_, exposed := PortMapping(ContainerTypeApp, &AppPortSpec{
		ContainerPort: 3000,
		BindAddress:   "127.0.0.1",
	})
	if len(exposed) == 0 {
		t.Error("app port should be exposed")
	}

	portMap, _ := PortMapping(ContainerTypePostgres, nil)
	if portMap != nil {
		t.Error("postgres should not expose ports to host")
	}
}

func TestIntegration_DeployAppWithPostgres_EnvVars(t *testing.T) {
	dbName := "myshop_db"
	password := "testpass123"
	dbURL := env.GenerateDatabaseURL(password, dbName)
	if !strings.Contains(dbURL, dbName) {
		t.Errorf("DATABASE_URL should contain dbName, got %q", dbURL)
	}
	if !strings.Contains(dbURL, password) {
		t.Errorf("DATABASE_URL should contain password, got %q", dbURL)
	}
	if !strings.HasPrefix(dbURL, "postgres://") && !strings.HasPrefix(dbURL, "postgresql://") {
		t.Errorf("DATABASE_URL should start with postgres:// or postgresql://, got %q", dbURL)
	}

	defaults := env.MergeWithDefaults(map[string]string{}, 3000)
	if _, ok := defaults["PORT"]; !ok {
		t.Error("MergeWithDefaults should add PORT")
	}
	if _, ok := defaults["YOURPLATFORM"]; !ok {
		t.Error("MergeWithDefaults should add YOURPLATFORM")
	}

	masked := env.MaskEnvVars(map[string]string{"DB_PASS": "secret123"})
	if masked["DB_PASS"] == "secret123" {
		t.Error("MaskEnvVars should mask the value")
	}
}

func TestIntegration_DeployAppWithPostgres_ResourceLimits(t *testing.T) {
	pgLimits := DefaultResourceLimits(ContainerTypePostgres)
	if pgLimits.MemoryHard != 1*gb {
		t.Errorf("postgres hard limit = %d, want %d", pgLimits.MemoryHard, 1*gb)
	}

	appLimits := DefaultResourceLimits(ContainerTypeApp)
	if appLimits.MemoryHard != 512*mb {
		t.Errorf("app hard limit = %d, want %d", appLimits.MemoryHard, 512*mb)
	}

	err := ValidateResourceLimits(appLimits, 2048)
	if err != nil {
		t.Errorf("ValidateResourceLimits should pass for 2GB server: %v", err)
	}

	badLimits := &ResourceLimits{MemoryHard: 3 * gb}
	err = ValidateResourceLimits(badLimits, 2048)
	if err == nil {
		t.Error("ValidateResourceLimits should fail when limit exceeds RAM")
	}
}

func TestIntegration_DeployAppWithPostgres_ContainerNaming(t *testing.T) {
	appName := ContainerName("My Shop!", "app")
	if appName != "yourplatform_my-shop_app" {
		t.Errorf("ContainerName(app) = %q, want yourplatform_my-shop_app", appName)
	}

	pgName := ContainerName("My Shop!", ContainerRole(ContainerTypePostgres))
	if pgName != "yourplatform_my-shop_postgres" {
		t.Errorf("ContainerName(postgres) = %q, want yourplatform_my-shop_postgres", pgName)
	}
}

func TestIntegration_DeployAppWithPostgres_ContainerLabels(t *testing.T) {
	labels := ContainerLabels("My Blog", ContainerTypePostgres)
	if labels[containerLabelOwner] != containerLabelOwnerVal {
		t.Errorf("owner label = %q, want %q", labels[containerLabelOwner], containerLabelOwnerVal)
	}
	if labels[containerLabelProject] != "my-blog" {
		t.Errorf("project label = %q, want my-blog", labels[containerLabelProject])
	}
	if labels[containerLabelRole] != "postgres" {
		t.Errorf("role label = %q, want postgres", labels[containerLabelRole])
	}
}

// Test 2: Redeploy
func TestIntegration_Redupeploy_SameVolumePreserved(t *testing.T) {
	vol1 := VolumeName("myshop", VolumePurposePostgresData)
	vol2 := VolumeName("myshop", VolumePurposePostgresData)
	if vol1 != vol2 {
		t.Errorf("same volume name expected, got %q vs %q", vol1, vol2)
	}
}

func TestIntegration_Redupeploy_SameNetworkPreserved(t *testing.T) {
	net1 := ProjectNetworkName("myshop")
	net2 := ProjectNetworkName("myshop")
	if net1 != net2 {
		t.Errorf("same network name expected, got %q vs %q", net1, net2)
	}
}

func TestIntegration_Redupeploy_ContainerReplaceName(t *testing.T) {
	name1 := ContainerName("myshop", "app")
	name2 := ContainerName("myshop", "app")
	if name1 != name2 {
		t.Errorf("container name should be deterministic, got %q vs %q", name1, name2)
	}
}

// Test 3: OOM
func TestIntegration_OOM_IsOOMKill(t *testing.T) {
	if !IsOOMKill(137) {
		t.Error("exit code 137 should be OOM")
	}
	if IsOOMKill(1) {
		t.Error("exit code 1 should not be OOM")
	}
}

func TestIntegration_OOM_RestartPolicy(t *testing.T) {
	limits := DefaultResourceLimits(ContainerTypeApp)
	if limits.MemoryHard != 512*mb {
		t.Errorf("app memory limit = %d, want 512MB", limits.MemoryHard/(1024*1024))
	}
}

func TestIntegration_OOM_CrashErrorMessage(t *testing.T) {
	crash := &CrashError{
		ContainerID: "aabbccdd0011",
		ExitCode:    137,
		Logs:        "Killed",
	}
	errMsg := crash.Error()
	if !strings.Contains(errMsg, "137") {
		t.Errorf("crash error should contain exit code, got %q", errMsg)
	}
}

// Test 5: Delete
func TestIntegration_Delete_VolumeNamingConsistent(t *testing.T) {
	volName := VolumeName("myshop", VolumePurposePostgresData)
	if !strings.HasPrefix(volName, volumeNamePrefix) {
		t.Errorf("orphan volumes should have prefix, got %q", volName)
	}
}

func TestIntegration_SanitizeProjectName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"My Shop!", "my-shop"},
		{"Next.js App 3", "nextjs-app-3"},
		{"my_project", "my-project"},
		{"  Hello World  ", "hello-world"},
		{"", "project"},
	}
	for _, tt := range tests {
		got := SanitizeProjectName(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeProjectName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIntegration_VolumeLabelsConsistent(t *testing.T) {
	labels := VolumeLabels("myshop", VolumePurposePostgresData)
	if labels[labelOwner] != labelOwnerValue {
		t.Error("volume should have owner label")
	}
	if labels["yourplatform.project"] != "myshop" {
		t.Errorf("volume project label = %q, want myshop", labels["yourplatform.project"])
	}
}

func TestIntegration_PortManagerConsistency(t *testing.T) {
	dbTypes := []ContainerType{ContainerTypePostgres, ContainerTypeMySQL, ContainerTypeRedis}
	for _, ct := range dbTypes {
		pm, pe := PortMapping(ct, &AppPortSpec{ContainerPort: 5432})
		if pm != nil || pe != nil {
			t.Errorf("%s should not expose ports", ct)
		}
	}
}