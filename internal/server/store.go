package server

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"insylus/internal/shared"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/ssh"
)

type Store struct {
	db              *sql.DB
	dbPath          string
	pluginSecretKey []byte
}

var ErrDuplicateDeviceName = errors.New("device name already exists")
var ErrDuplicateTopologyLink = errors.New("topology link already exists")

type DeviceRecord struct {
	Device    shared.Device
	Policy    shared.Policy
	Report    shared.DeviceReport
	Metadata  DeviceMetadata
	Discovery DeviceDiscoverySnapshot
	Resolved  ResolvedTopology
	Update    AgentUpdateState
}

type DeviceMetadata struct {
	DeviceID               string
	Note                   string
	TypeOverride           *shared.DeviceType
	PurposeOverride        *shared.DevicePurpose
	ParentOverrideDeviceID *string
	ParentOverrideState    shared.ParentOverrideState
	UpdatedAt              time.Time
}

type DeviceDiscoverySnapshot struct {
	DeviceID        string
	DeviceType      shared.DeviceType
	Purpose         shared.DevicePurpose
	PlatformClass   shared.PlatformClass
	WakeOnLAN       shared.WakeOnLANInfo
	Workloads       []shared.Workload
	ChildCandidates []shared.ChildCandidate
	Warnings        []string
	UpdatedAt       time.Time
}

type ResolvedTopology struct {
	DeviceID            string
	EffectiveDeviceType shared.DeviceType
	DeviceTypeSource    shared.TopologySource
	Purpose             shared.DevicePurpose
	PurposeSource       shared.TopologySource
	PlatformClass       shared.PlatformClass
	ParentDeviceID      string
	ParentName          string
	ParentState         shared.ParentState
	ParentSource        shared.TopologySource
	MatchReason         string
	MatchConfidence     string
	Children            []string
	UpdatedAt           time.Time
}

type AgentUpdateState struct {
	DeviceID           string
	Override           shared.AgentAutoUpdateOverride
	EffectiveEnabled   bool
	UpdateAvailable    bool
	ServerAgentVersion string
	Status             shared.AgentUpdateStatus
	Error              string
	LastCheckedAt      time.Time
	LastAttemptedAt    time.Time
	ReportedGOOS       string
	ReportedGOARCH     string
}

type ManualTopologyNode struct {
	ID        int64
	Name      string
	Kind      shared.TopologyNodeKind
	Note      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ManualTopologyLink struct {
	ID        int64
	FromKind  shared.TopologyEndpointKind
	FromID    string
	ToKind    shared.TopologyEndpointKind
	ToID      string
	Label     string
	Source    shared.TopologySource
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ServiceInstanceRecord struct {
	ID              int64
	Device          shared.Device
	NormalizedName  string
	Name            string
	Kind            shared.WorkloadKind
	Image           string
	State           string
	DiscoveredState string
	Health          shared.ServiceHealth
	Endpoints       []shared.Endpoint
	FirstSeenAt     time.Time
	LastSeenAt      time.Time
	MissingSince    time.Time
	LastReportedAt  time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ServiceEventRecord struct {
	ID          int64
	Action      string
	ServiceName string
	ServiceKind shared.WorkloadKind
	Device      shared.Device
	Details     string
	CreatedAt   time.Time
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, dbPath: path}
	if err := store.migrate(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func loadOrCreatePluginSecretKey(dbPath string) ([]byte, error) {
	if dbPath == "" || dbPath == ":memory:" || strings.HasPrefix(dbPath, "file::memory:") {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		return key, nil
	}
	keyPath := filepath.Join(filepath.Dir(dbPath), "plugin_secrets.key")
	legacyKeyPath := filepath.Join(filepath.Dir(dbPath), "proxmox_tokens.key")
	if _, err := os.Stat(keyPath); errors.Is(err, os.ErrNotExist) {
		if _, legacyErr := os.Stat(legacyKeyPath); legacyErr == nil {
			_ = os.Rename(legacyKeyPath, keyPath)
		}
	}
	if key, err := os.ReadFile(keyPath); err == nil {
		keyText := strings.TrimSpace(string(key))
		decoded, err := base64.StdEncoding.DecodeString(keyText)
		if err != nil {
			return nil, fmt.Errorf("decode plugin secret key: %w", err)
		}
		if len(decoded) != 32 {
			return nil, fmt.Errorf("invalid plugin secret key length")
		}
		return decoded, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(keyPath, []byte(encoded+"\n"), 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Store) encryptPluginSecret(secret string) (string, error) {
	if err := s.ensurePluginSecretKey(); err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.pluginSecretKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(secret), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *Store) decryptPluginSecret(encoded string) (string, error) {
	if err := s.ensurePluginSecretKey(); err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.pluginSecretKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid plugin secret ciphertext")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (s *Store) ensurePluginSecretKey() error {
	if len(s.pluginSecretKey) == 32 {
		return nil
	}
	key, err := loadOrCreatePluginSecretKey(s.dbPath)
	if err != nil {
		return err
	}
	s.pluginSecretKey = key
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`create table if not exists devices (
			id text primary key,
			name text not null,
			bootstrap_token text not null unique,
			agent_token text not null default '',
			hostname text not null default '',
			os_name text not null default '',
			ips_json text not null default '[]',
			agent_version text not null default '',
			last_seen_at text,
			created_at text not null,
			updated_at text not null
		);`,
		`create unique index if not exists devices_name_unique on devices(name collate nocase);`,
		`create table if not exists ssh_keys (
			id integer primary key autoincrement,
			name text not null unique,
			public_key text not null,
			fingerprint text not null,
			created_at text not null
		);`,
		`create table if not exists device_access_policies (
			device_id text primary key references devices(id) on delete cascade,
			device_mode text not null default 'inventory-only',
			managed_account_enabled integer not null default 0,
			access_mode text not null default 'disabled',
			ssh_key_id integer,
			policy_revision integer not null default 1,
			updated_at text not null
		);`,
		`create table if not exists device_reports (
			device_id text primary key references devices(id) on delete cascade,
			applied_revision integer not null default 0,
			user_present integer not null default 0,
			sudo_enabled integer not null default 0,
			audit_enabled integer not null default 0,
			authorized_fingerprints_json text not null default '[]',
			enforcement_succeeded integer not null default 0,
			error_message text not null default '',
			health_json text not null default '{}',
			updated_at text not null
		);`,
		`create table if not exists device_metadata (
			device_id text primary key references devices(id) on delete cascade,
			note text not null default '',
			type_override text,
			purpose_override text,
			parent_override_device_id text references devices(id) on delete set null,
			parent_override_state text not null default 'inherit',
			updated_at text not null
		);`,
		`create table if not exists device_discovery_snapshots (
			device_id text primary key references devices(id) on delete cascade,
			device_type text not null default 'unknown',
			purpose text not null default 'unknown',
			platform_class text not null default 'unknown',
			wol_json text not null default '{}',
			workloads_json text not null default '[]',
			child_candidates_json text not null default '[]',
			warnings_json text not null default '[]',
			updated_at text not null
		);`,
		`create table if not exists relationship_candidates (
			child_device_id text primary key references devices(id) on delete cascade,
			parent_device_id text references devices(id) on delete cascade,
			confidence text not null default '',
			reason text not null default '',
			updated_at text not null
		);`,
		`create table if not exists app_settings (
			key text primary key,
			value text not null,
			updated_at text not null
		);`,
		`create table if not exists targets (
			id text primary key,
			name text not null,
			kind text not null default 'target',
			hostname text not null default '',
			ips_json text not null default '[]',
			tags_json text not null default '[]',
			note text not null default '',
			created_by text not null default '',
			created_at text not null,
			updated_at text not null
		);`,
		`create unique index if not exists targets_name_unique on targets(name collate nocase);`,
		`create table if not exists target_addresses (
			target_id text not null references targets(id) on delete cascade,
			kind text not null,
			value text not null,
			created_at text not null,
			updated_at text not null,
			primary key(target_id, kind)
		);`,
		`create table if not exists target_metadata (
			target_id text not null references targets(id) on delete cascade,
			plugin_id text not null,
			metadata_json text not null default '{}',
			updated_at text not null,
			primary key(target_id, plugin_id)
		);`,
		`create table if not exists plugin_settings (
			plugin_id text primary key,
			enabled integer not null default 0,
			settings_json text not null default '{}',
			created_at text not null,
			updated_at text not null
		);`,
		`create table if not exists plugin_secrets (
			plugin_id text not null,
			name text not null,
			ciphertext text not null,
			created_at text not null,
			updated_at text not null,
			primary key(plugin_id, name)
		);`,
		`create table if not exists device_agent_updates (
			device_id text primary key references devices(id) on delete cascade,
			auto_update_override text not null default 'inherit',
			effective_enabled integer not null default 0,
			update_available integer not null default 0,
			server_agent_version text not null default '',
			status text not null default 'idle',
			error text not null default '',
			last_checked_at text,
			last_attempted_at text,
			reported_goos text not null default '',
			reported_goarch text not null default '',
			updated_at text not null
		);`,
		`create table if not exists topology_nodes (
			id integer primary key autoincrement,
			name text not null,
			kind text not null,
			note text not null default '',
			created_at text not null,
			updated_at text not null
		);`,
		`create table if not exists topology_links (
			id integer primary key autoincrement,
			from_kind text not null,
			from_id text not null,
			to_kind text not null,
			to_id text not null,
			label text not null default '',
			source text not null default 'manual',
			created_at text not null,
			updated_at text not null
		);`,
		`create unique index if not exists topology_links_manual_unique
			on topology_links(from_kind, from_id, to_kind, to_id, label)
			where source = 'manual';`,
		`create table if not exists service_instances (
			id integer primary key autoincrement,
			device_id text not null references devices(id) on delete cascade,
			normalized_name text not null,
			name text not null,
			kind text not null,
			image text not null default '',
			state text not null default '',
			discovered_state text not null default '',
			health text not null default 'unknown',
			endpoints_json text not null default '[]',
			first_seen_at text not null,
			last_seen_at text,
			missing_since text,
			last_reported_at text not null,
			created_at text not null,
			updated_at text not null
		);`,
		`create unique index if not exists service_instances_device_kind_name_unique
			on service_instances(device_id, kind, normalized_name);`,
		`create index if not exists service_instances_name_idx
			on service_instances(normalized_name);`,
		`create table if not exists service_events (
			id integer primary key autoincrement,
			service_instance_id integer,
			device_id text not null references devices(id) on delete cascade,
			service_name text not null,
			service_kind text not null,
			action text not null,
			details text not null default '',
			created_at text not null
		);`,
		`create index if not exists service_events_created_idx on service_events(created_at desc);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.ensureColumn(ctx, "device_discovery_snapshots", "purpose", "text not null default 'unknown'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "device_discovery_snapshots", "wol_json", "text not null default '{}'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "device_access_policies", "device_mode", "text not null default 'inventory-only'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "device_access_policies", "managed_account_enabled", "integer not null default 0"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "device_metadata", "purpose_override", "text"); err != nil {
		return err
	}
	backfills := []string{
		`insert into device_metadata (device_id, updated_at)
		 select d.id, coalesce(d.updated_at, d.created_at, ?)
		 from devices d
		 left join device_metadata m on m.device_id = d.id
		 where m.device_id is null;`,
		`insert into device_discovery_snapshots (device_id, updated_at)
		 select d.id, coalesce(d.updated_at, d.created_at, ?)
		 from devices d
		 left join device_discovery_snapshots ds on ds.device_id = d.id
		 where ds.device_id is null;`,
		`insert into relationship_candidates (child_device_id, updated_at)
		 select d.id, coalesce(d.updated_at, d.created_at, ?)
		 from devices d
		 left join relationship_candidates rc on rc.child_device_id = d.id
		 where rc.child_device_id is null;`,
		`insert into device_agent_updates (device_id, updated_at)
		 select d.id, coalesce(d.updated_at, d.created_at, ?)
		 from devices d
		 left join device_agent_updates au on au.device_id = d.id
		 where au.device_id is null;`,
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, stmt := range backfills {
		if _, err := s.db.ExecContext(ctx, stmt, now); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `
		insert into app_settings (key, value, updated_at)
		values ('agent_auto_update_default', 'false', ?)
		on conflict(key) do nothing`, now); err != nil {
		return err
	}
	if err := s.backfillServiceInstances(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) backfillServiceInstances(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		select device_id, workloads_json, updated_at
		from device_discovery_snapshots
		order by device_id asc`)
	if err != nil {
		return err
	}
	type snapshot struct {
		deviceID  string
		workloads []shared.Workload
		updatedAt time.Time
	}
	var snapshots []snapshot
	for rows.Next() {
		var deviceID, workloadsJSON, updatedAtText string
		if err := rows.Scan(&deviceID, &workloadsJSON, &updatedAtText); err != nil {
			_ = rows.Close()
			return err
		}
		var workloads []shared.Workload
		if workloadsJSON != "" {
			_ = json.Unmarshal([]byte(workloadsJSON), &workloads)
		}
		updatedAt, _ := time.Parse(time.RFC3339, updatedAtText)
		snapshots = append(snapshots, snapshot{deviceID: deviceID, workloads: workloads, updatedAt: updatedAt})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		if err := s.syncServiceInstances(ctx, snapshot.deviceID, snapshot.workloads, snapshot.updatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureColumn(ctx context.Context, tableName, columnName, columnDef string) error {
	rows, err := s.db.QueryContext(ctx, `pragma table_info(`+tableName+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			typ     string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if strings.EqualFold(name, columnName) {
			return nil
		}
	}
	if rows.Err() != nil {
		return rows.Err()
	}
	_, err = s.db.ExecContext(ctx, `alter table `+tableName+` add column `+columnName+` `+columnDef)
	return err
}

func (s *Store) CreateDevice(ctx context.Context, name string) (shared.Device, error) {
	now := time.Now().UTC()
	device := shared.Device{
		ID:             randomToken(12),
		Name:           name,
		BootstrapToken: randomToken(24),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return shared.Device{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		insert into devices (id, name, bootstrap_token, created_at, updated_at)
		values (?, ?, ?, ?, ?)`,
		device.ID, device.Name, device.BootstrapToken, now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		if isUniqueConstraint(err, "devices_name_unique") {
			return shared.Device{}, ErrDuplicateDeviceName
		}
		return shared.Device{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		insert into device_access_policies (device_id, device_mode, managed_account_enabled, access_mode, policy_revision, updated_at)
		values (?, 'inventory-only', 0, 'disabled', 1, ?)`,
		device.ID, now.Format(time.RFC3339)); err != nil {
		return shared.Device{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		insert into device_reports (device_id, updated_at)
		values (?, ?)`, device.ID, now.Format(time.RFC3339)); err != nil {
		return shared.Device{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		insert into device_metadata (device_id, updated_at)
		values (?, ?)`, device.ID, now.Format(time.RFC3339)); err != nil {
		return shared.Device{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		insert into device_discovery_snapshots (device_id, updated_at)
		values (?, ?)`, device.ID, now.Format(time.RFC3339)); err != nil {
		return shared.Device{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		insert into relationship_candidates (child_device_id, updated_at)
		values (?, ?)`, device.ID, now.Format(time.RFC3339)); err != nil {
		return shared.Device{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		insert into device_agent_updates (device_id, updated_at)
		values (?, ?)`, device.ID, now.Format(time.RFC3339)); err != nil {
		return shared.Device{}, err
	}
	emptyJSON := "[]"
	if _, err := tx.ExecContext(ctx, `
		insert into targets (id, name, kind, hostname, ips_json, tags_json, note, created_by, created_at, updated_at)
		values (?, ?, 'linux-host', '', ?, ?, '', 'devices', ?, ?)
		on conflict(id) do nothing`,
		device.ID, device.Name, emptyJSON, emptyJSON, now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		return shared.Device{}, err
	}
	if err := tx.Commit(); err != nil {
		return shared.Device{}, err
	}
	return device, nil
}

func (s *Store) ListDevices(ctx context.Context) ([]DeviceRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		select
			d.id, d.name, d.bootstrap_token, d.agent_token, d.hostname, d.os_name, d.ips_json, d.agent_version, d.last_seen_at, d.created_at, d.updated_at,
			coalesce(p.device_mode, 'inventory-only'), p.managed_account_enabled, p.access_mode, p.ssh_key_id, p.policy_revision, p.updated_at,
			coalesce(k.name, ''), coalesce(k.public_key, ''), coalesce(k.fingerprint, ''),
			r.applied_revision, r.user_present, r.sudo_enabled, r.audit_enabled, r.authorized_fingerprints_json, r.enforcement_succeeded, r.error_message, r.health_json, r.updated_at,
			coalesce(m.note, ''), m.type_override, m.purpose_override, m.parent_override_device_id, coalesce(m.parent_override_state, 'inherit'), m.updated_at,
			coalesce(ds.device_type, 'unknown'), coalesce(ds.purpose, 'unknown'), coalesce(ds.platform_class, 'unknown'), coalesce(ds.wol_json, '{}'), coalesce(ds.workloads_json, '[]'), coalesce(ds.child_candidates_json, '[]'), coalesce(ds.warnings_json, '[]'), ds.updated_at,
			coalesce(rc.parent_device_id, ''), coalesce(parent.name, ''), coalesce(op.name, ''), coalesce(rc.confidence, ''), coalesce(rc.reason, ''), rc.updated_at,
			coalesce(au.auto_update_override, 'inherit'), coalesce(au.effective_enabled, 0), coalesce(au.update_available, 0), coalesce(au.server_agent_version, ''), coalesce(au.status, 'idle'), coalesce(au.error, ''), au.last_checked_at, au.last_attempted_at, coalesce(au.reported_goos, ''), coalesce(au.reported_goarch, '')
		from devices d
		left join device_access_policies p on p.device_id = d.id
		left join ssh_keys k on k.id = p.ssh_key_id
		left join device_reports r on r.device_id = d.id
		left join device_metadata m on m.device_id = d.id
		left join device_discovery_snapshots ds on ds.device_id = d.id
		left join relationship_candidates rc on rc.child_device_id = d.id
		left join device_agent_updates au on au.device_id = d.id
		left join devices parent on parent.id = rc.parent_device_id
		left join devices op on op.id = m.parent_override_device_id
		order by d.name asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceRecord
	for rows.Next() {
		record, err := scanDeviceRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *Store) GetDevice(ctx context.Context, id string) (DeviceRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		select
			d.id, d.name, d.bootstrap_token, d.agent_token, d.hostname, d.os_name, d.ips_json, d.agent_version, d.last_seen_at, d.created_at, d.updated_at,
			coalesce(p.device_mode, 'inventory-only'), p.managed_account_enabled, p.access_mode, p.ssh_key_id, p.policy_revision, p.updated_at,
			coalesce(k.name, ''), coalesce(k.public_key, ''), coalesce(k.fingerprint, ''),
			r.applied_revision, r.user_present, r.sudo_enabled, r.audit_enabled, r.authorized_fingerprints_json, r.enforcement_succeeded, r.error_message, r.health_json, r.updated_at,
			coalesce(m.note, ''), m.type_override, m.purpose_override, m.parent_override_device_id, coalesce(m.parent_override_state, 'inherit'), m.updated_at,
			coalesce(ds.device_type, 'unknown'), coalesce(ds.purpose, 'unknown'), coalesce(ds.platform_class, 'unknown'), coalesce(ds.wol_json, '{}'), coalesce(ds.workloads_json, '[]'), coalesce(ds.child_candidates_json, '[]'), coalesce(ds.warnings_json, '[]'), ds.updated_at,
			coalesce(rc.parent_device_id, ''), coalesce(parent.name, ''), coalesce(op.name, ''), coalesce(rc.confidence, ''), coalesce(rc.reason, ''), rc.updated_at,
			coalesce(au.auto_update_override, 'inherit'), coalesce(au.effective_enabled, 0), coalesce(au.update_available, 0), coalesce(au.server_agent_version, ''), coalesce(au.status, 'idle'), coalesce(au.error, ''), au.last_checked_at, au.last_attempted_at, coalesce(au.reported_goos, ''), coalesce(au.reported_goarch, '')
		from devices d
		left join device_access_policies p on p.device_id = d.id
		left join ssh_keys k on k.id = p.ssh_key_id
		left join device_reports r on r.device_id = d.id
		left join device_metadata m on m.device_id = d.id
		left join device_discovery_snapshots ds on ds.device_id = d.id
		left join relationship_candidates rc on rc.child_device_id = d.id
		left join device_agent_updates au on au.device_id = d.id
		left join devices parent on parent.id = rc.parent_device_id
		left join devices op on op.id = m.parent_override_device_id
		where d.id = ?`, id)
	record, err := scanDeviceRecord(row)
	if err != nil {
		return DeviceRecord{}, err
	}
	return record, nil
}

func (s *Store) CreateSSHKey(ctx context.Context, name, publicKey string) (shared.SSHKey, error) {
	fingerprint, err := fingerprintAuthorizedKey(publicKey)
	if err != nil {
		return shared.SSHKey{}, err
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		insert into ssh_keys (name, public_key, fingerprint, created_at)
		values (?, ?, ?, ?)`, name, publicKey, fingerprint, now.Format(time.RFC3339))
	if err != nil {
		return shared.SSHKey{}, err
	}
	id, _ := res.LastInsertId()
	return shared.SSHKey{ID: id, Name: name, PublicKey: publicKey, Fingerprint: fingerprint, CreatedAt: now}, nil
}

func (s *Store) ListSSHKeys(ctx context.Context) ([]shared.SSHKey, error) {
	rows, err := s.db.QueryContext(ctx, `select id, name, public_key, fingerprint, created_at from ssh_keys order by name asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []shared.SSHKey
	for rows.Next() {
		var key shared.SSHKey
		var createdAt string
		if err := rows.Scan(&key.ID, &key.Name, &key.PublicKey, &key.Fingerprint, &createdAt); err != nil {
			return nil, err
		}
		key.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) UpdatePolicy(ctx context.Context, deviceID string, enabled bool, mode shared.AccessMode, sshKeyID *int64) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		update device_access_policies
		set managed_account_enabled = ?, access_mode = ?, ssh_key_id = ?, policy_revision = policy_revision + 1, updated_at = ?
		where device_id = ?`, boolInt(enabled), string(mode), sshKeyID, now.Format(time.RFC3339), deviceID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return s.resolveTopology(ctx)
}

func (s *Store) SetDeviceMode(ctx context.Context, deviceID string, mode shared.DeviceMode) error {
	if mode != shared.DeviceModeInventoryOnly && mode != shared.DeviceModeAccessManaged {
		return fmt.Errorf("invalid device mode: %s", mode)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var (
		res sql.Result
		err error
	)
	if mode == shared.DeviceModeAccessManaged {
		res, err = s.db.ExecContext(ctx, `
			update device_access_policies
			set device_mode = ?, managed_account_enabled = 0, access_mode = 'disabled', ssh_key_id = null, policy_revision = policy_revision + 1, updated_at = ?
			where device_id = ? and coalesce(device_mode, 'inventory-only') <> ?`,
			string(mode), now, deviceID, string(mode))
	} else {
		res, err = s.db.ExecContext(ctx, `
			update device_access_policies
			set device_mode = ?, policy_revision = policy_revision + 1, updated_at = ?
			where device_id = ? and coalesce(device_mode, 'inventory-only') <> ?`,
			string(mode), now, deviceID, string(mode))
	}
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		var exists int
		err := s.db.QueryRowContext(ctx, `select count(*) from device_access_policies where device_id = ?`, deviceID).Scan(&exists)
		if err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
	}
	return nil
}

func (s *Store) UpdateDeviceNote(ctx context.Context, deviceID, note string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		update device_metadata
		set note = ?, updated_at = ?
		where device_id = ?`, strings.TrimSpace(note), now, deviceID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	if _, err := s.db.ExecContext(ctx, `
		update targets
		set note = ?, updated_at = ?
		where id = ?`, strings.TrimSpace(note), now, deviceID); err != nil {
		return err
	}
	return nil
}

func (s *Store) SetTypeOverride(ctx context.Context, deviceID string, deviceType *shared.DeviceType) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var value any
	if deviceType != nil {
		value = string(*deviceType)
	}
	res, err := s.db.ExecContext(ctx, `
		update device_metadata
		set type_override = ?, updated_at = ?
		where device_id = ?`, value, now, deviceID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return s.resolveTopology(ctx)
}

func (s *Store) SetPurposeOverride(ctx context.Context, deviceID string, purpose *shared.DevicePurpose) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var value any
	if purpose != nil {
		value = string(*purpose)
	}
	res, err := s.db.ExecContext(ctx, `
		update device_metadata
		set purpose_override = ?, updated_at = ?
		where device_id = ?`, value, now, deviceID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SetParentOverride(ctx context.Context, deviceID string, state shared.ParentOverrideState, parentDeviceID *string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		update device_metadata
		set parent_override_state = ?, parent_override_device_id = ?, updated_at = ?
		where device_id = ?`, state, parentDeviceID, now, deviceID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return s.resolveTopology(ctx)
}

func (s *Store) CreateTopologyNode(ctx context.Context, name string, kind shared.TopologyNodeKind, note string) (ManualTopologyNode, error) {
	name = strings.TrimSpace(name)
	note = strings.TrimSpace(note)
	if name == "" {
		return ManualTopologyNode{}, fmt.Errorf("topology node name is required")
	}
	if !validManualTopologyNodeKind(kind) {
		return ManualTopologyNode{}, fmt.Errorf("invalid topology node kind: %s", kind)
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		insert into topology_nodes (name, kind, note, created_at, updated_at)
		values (?, ?, ?, ?, ?)`,
		name, string(kind), note, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return ManualTopologyNode{}, err
	}
	id, _ := res.LastInsertId()
	return ManualTopologyNode{ID: id, Name: name, Kind: kind, Note: note, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) ListTopologyNodes(ctx context.Context) ([]ManualTopologyNode, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, name, kind, note, created_at, updated_at
		from topology_nodes
		order by kind asc, name asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ManualTopologyNode
	for rows.Next() {
		node, err := scanTopologyNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

func (s *Store) DeleteTopologyNode(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	idText := fmt.Sprint(id)
	if _, err := tx.ExecContext(ctx, `
		delete from topology_links
		where (from_kind = 'topology_node' and from_id = ?)
		   or (to_kind = 'topology_node' and to_id = ?)`, idText, idText); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `delete from topology_nodes where id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) CreateTopologyLink(ctx context.Context, fromKind shared.TopologyEndpointKind, fromID string, toKind shared.TopologyEndpointKind, toID string, label string) (ManualTopologyLink, error) {
	fromID = strings.TrimSpace(fromID)
	toID = strings.TrimSpace(toID)
	label = strings.TrimSpace(label)
	if err := s.validateTopologyEndpoint(ctx, fromKind, fromID); err != nil {
		return ManualTopologyLink{}, fmt.Errorf("invalid from endpoint: %w", err)
	}
	if err := s.validateTopologyEndpoint(ctx, toKind, toID); err != nil {
		return ManualTopologyLink{}, fmt.Errorf("invalid to endpoint: %w", err)
	}
	if fromKind == toKind && fromID == toID {
		return ManualTopologyLink{}, fmt.Errorf("topology link endpoints must differ")
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		insert into topology_links (from_kind, from_id, to_kind, to_id, label, source, created_at, updated_at)
		values (?, ?, ?, ?, ?, 'manual', ?, ?)`,
		string(fromKind), fromID, string(toKind), toID, label, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		if isUniqueConstraint(err, "topology_links_manual_unique") {
			return ManualTopologyLink{}, ErrDuplicateTopologyLink
		}
		return ManualTopologyLink{}, err
	}
	id, _ := res.LastInsertId()
	return ManualTopologyLink{
		ID:        id,
		FromKind:  fromKind,
		FromID:    fromID,
		ToKind:    toKind,
		ToID:      toID,
		Label:     label,
		Source:    shared.TopologySourceManual,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *Store) ListTopologyLinks(ctx context.Context) ([]ManualTopologyLink, error) {
	rows, err := s.db.QueryContext(ctx, `
		select id, from_kind, from_id, to_kind, to_id, label, source, created_at, updated_at
		from topology_links
		order by source asc, id asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ManualTopologyLink
	for rows.Next() {
		link, err := scanTopologyLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

func (s *Store) DeleteTopologyLink(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `delete from topology_links where id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) validateTopologyEndpoint(ctx context.Context, kind shared.TopologyEndpointKind, id string) error {
	if id == "" {
		return fmt.Errorf("endpoint id is required")
	}
	switch kind {
	case shared.TopologyEndpointDevice:
		var exists int
		if err := s.db.QueryRowContext(ctx, `select count(*) from devices where id = ?`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
	case shared.TopologyEndpointTopologyNode:
		var exists int
		if err := s.db.QueryRowContext(ctx, `select count(*) from topology_nodes where id = ?`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
	default:
		return fmt.Errorf("invalid endpoint kind: %s", kind)
	}
	return nil
}

func (s *Store) AgentAutoUpdateDefault(ctx context.Context) (bool, error) {
	row := s.db.QueryRowContext(ctx, `select value from app_settings where key = 'agent_auto_update_default'`)
	var value string
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return strings.EqualFold(value, "true") || value == "1", nil
}

func (s *Store) ManagedAccountConfig(ctx context.Context, fallback shared.ManagedAccountConfig) (shared.ManagedAccountConfig, error) {
	user, err := s.appSetting(ctx, "managed_user")
	if err != nil {
		return shared.ManagedAccountConfig{}, err
	}
	groupsRaw, err := s.appSetting(ctx, "managed_groups")
	if err != nil {
		return shared.ManagedAccountConfig{}, err
	}
	accessModeRaw, err := s.appSetting(ctx, "managed_access_mode")
	if err != nil {
		return shared.ManagedAccountConfig{}, err
	}
	cfg := shared.ManagedAccountConfig{
		ManagedUser:   strings.TrimSpace(user),
		ManagedGroups: splitManagedGroups(groupsRaw),
		AccessMode:    shared.AccessMode(accessModeRaw),
	}
	if cfg.ManagedUser == "" {
		cfg.ManagedUser = strings.TrimSpace(fallback.ManagedUser)
	}
	if cfg.ManagedUser == "" {
		cfg.ManagedUser = shared.DefaultManagedUser
	}
	if len(cfg.ManagedGroups) == 0 {
		cfg.ManagedGroups = append([]string(nil), fallback.ManagedGroups...)
	}
	if len(cfg.ManagedGroups) == 0 {
		cfg.ManagedGroups = []string{"adm", "systemd-journal"}
	}
	if cfg.AccessMode == "" {
		cfg.AccessMode = shared.AccessModeAudit
	}
	return cfg, nil
}

func (s *Store) SetManagedAccountConfig(ctx context.Context, cfg shared.ManagedAccountConfig) error {
	cfg.ManagedUser = strings.TrimSpace(cfg.ManagedUser)
	if cfg.ManagedUser == "" {
		return fmt.Errorf("managed user is required")
	}
	if cfg.AccessMode == "" {
		cfg.AccessMode = shared.AccessModeAudit
	}
	if cfg.AccessMode != shared.AccessModeAudit && cfg.AccessMode != shared.AccessModeDocker && cfg.AccessMode != shared.AccessModeSudoPrompted && cfg.AccessMode != shared.AccessModeSudoPasswordless {
		return fmt.Errorf("invalid access mode: %s", cfg.AccessMode)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		insert into app_settings (key, value, updated_at)
		values ('managed_user', ?, ?)
		on conflict(key) do update set value = excluded.value, updated_at = excluded.updated_at`, cfg.ManagedUser, now); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		insert into app_settings (key, value, updated_at)
		values ('managed_access_mode', ?, ?)
		on conflict(key) do update set value = excluded.value, updated_at = excluded.updated_at`, string(cfg.AccessMode), now); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) appSetting(ctx context.Context, key string) (string, error) {
	row := s.db.QueryRowContext(ctx, `select value from app_settings where key = ?`, key)
	var value string
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func splitManagedGroups(raw string) []string {
	parts := strings.Split(raw, ",")
	groups := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		group := strings.TrimSpace(part)
		if group == "" {
			continue
		}
		key := strings.ToLower(group)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		groups = append(groups, group)
	}
	return groups
}

func (s *Store) SetAgentAutoUpdateDefault(ctx context.Context, enabled bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
		insert into app_settings (key, value, updated_at)
		values ('agent_auto_update_default', ?, ?)
		on conflict(key) do update set value = excluded.value, updated_at = excluded.updated_at`, boolString(enabled), now); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		update device_agent_updates
		set effective_enabled = ?, updated_at = ?
		where auto_update_override = 'inherit'`, boolInt(enabled), now)
	return err
}

func (s *Store) SetAgentAutoUpdateOverride(ctx context.Context, deviceID string, override shared.AgentAutoUpdateOverride) error {
	if override == "" {
		override = shared.AgentAutoUpdateInherit
	}
	if override != shared.AgentAutoUpdateInherit && override != shared.AgentAutoUpdateEnabled && override != shared.AgentAutoUpdateDisabled {
		return fmt.Errorf("invalid auto-update override: %s", override)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		update device_agent_updates
		set auto_update_override = ?, updated_at = ?
		where device_id = ?`, string(override), now, deviceID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	globalEnabled, err := s.AgentAutoUpdateDefault(ctx)
	if err != nil {
		return err
	}
	effective := effectiveAgentAutoUpdateStore(globalEnabled, override)
	_, err = s.db.ExecContext(ctx, `
		update device_agent_updates
		set effective_enabled = ?, updated_at = ?
		where device_id = ?`, boolInt(effective), now, deviceID)
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) SaveAgentUpdateStatus(ctx context.Context, deviceID string, report shared.AgentUpdateReport) error {
	status := report.Status
	if status == "" {
		status = shared.AgentUpdateStatusIdle
	}
	now := time.Now().UTC().Format(time.RFC3339)
	lastAttempted := nullableTime(report.AttemptedAt)
	_, err := s.db.ExecContext(ctx, `
		update device_agent_updates
		set status = ?, error = ?, server_agent_version = coalesce(nullif(?, ''), server_agent_version), last_attempted_at = coalesce(?, last_attempted_at), updated_at = ?
		where device_id = ?`,
		string(status), report.Error, report.ServerAgentVersion, lastAttempted, now, deviceID)
	return err
}

func (s *Store) RecordAgentUpdateCheck(ctx context.Context, deviceID string, enabled, available bool, serverVersion, status, errText, goos, goarch string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if status == "" {
		status = string(shared.AgentUpdateStatusIdle)
	}
	_, err := s.db.ExecContext(ctx, `
		update device_agent_updates
		set effective_enabled = ?, update_available = ?, server_agent_version = ?, status = ?, error = ?, last_checked_at = ?, reported_goos = ?, reported_goarch = ?, updated_at = ?
		where device_id = ?`,
		boolInt(enabled), boolInt(available), serverVersion, status, errText, now, goos, goarch, now, deviceID)
	return err
}

func (s *Store) RegisterAgent(ctx context.Context, req shared.BootstrapRequest) (shared.BootstrapResponse, error) {
	row := s.db.QueryRowContext(ctx, `select id, agent_token from devices where bootstrap_token = ?`, req.BootstrapToken)
	var deviceID, agentToken string
	if err := row.Scan(&deviceID, &agentToken); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return shared.BootstrapResponse{}, fmt.Errorf("unknown bootstrap token")
		}
		return shared.BootstrapResponse{}, err
	}
	if agentToken == "" {
		agentToken = randomToken(32)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
		update devices set agent_token = ?, hostname = ?, os_name = ?, agent_version = ?, updated_at = ?
		where id = ?`,
		agentToken, req.Hostname, req.OSName, req.AgentVersion, now, deviceID); err != nil {
		return shared.BootstrapResponse{}, err
	}
	return shared.BootstrapResponse{
		DeviceID:   deviceID,
		AgentToken: agentToken,
		Interval:   shared.AgentCheckInInterval.String(),
	}, nil
}

func (s *Store) AuthenticateAgent(ctx context.Context, token string) (shared.Device, error) {
	row := s.db.QueryRowContext(ctx, `
		select id, name, bootstrap_token, agent_token, hostname, os_name, ips_json, agent_version, last_seen_at, created_at, updated_at
		from devices where agent_token = ?`, token)
	device, err := scanDevice(row)
	if err != nil {
		return shared.Device{}, err
	}
	return device, nil
}

func (s *Store) UpdateCheckIn(ctx context.Context, deviceID string, health shared.HealthSnapshot) error {
	now := time.Now().UTC()
	ipsJSON, _ := json.Marshal(health.IPs)
	_, err := s.db.ExecContext(ctx, `
		update devices
		set hostname = ?, os_name = ?, ips_json = ?, agent_version = ?, last_seen_at = ?, updated_at = ?
		where id = ?`,
		health.Hostname, health.OSName, string(ipsJSON), health.AgentVersion, now.Format(time.RFC3339), now.Format(time.RFC3339), deviceID)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		update device_agent_updates
		set reported_goos = ?, reported_goarch = ?, updated_at = ?
		where device_id = ?`, health.AgentGOOS, health.AgentGOARCH, now.Format(time.RFC3339), deviceID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		update targets
		set hostname = ?, ips_json = ?, updated_at = ?
		where id = ?`,
		health.Hostname, string(ipsJSON), now.Format(time.RFC3339), deviceID); err != nil {
		return err
	}
	return s.resolveTopology(ctx)
}

func (s *Store) GetPolicyForDevice(ctx context.Context, deviceID string) (shared.AgentPolicyResponse, error) {
	row := s.db.QueryRowContext(ctx, `
		select p.device_id, coalesce(p.device_mode, 'inventory-only'), p.managed_account_enabled, p.access_mode, p.ssh_key_id, coalesce(k.public_key, ''), coalesce(k.fingerprint, ''), p.policy_revision
		from device_access_policies p
		left join ssh_keys k on k.id = p.ssh_key_id
		where p.device_id = ?`, deviceID)
	var policy shared.AgentPolicyResponse
	var enabled int
	if err := row.Scan(&policy.DeviceID, &policy.DeviceMode, &enabled, &policy.AccessMode, &policy.AssignedKeyID, &policy.AssignedKey, &policy.KeyFingerprint, &policy.PolicyRevision); err != nil {
		return shared.AgentPolicyResponse{}, err
	}
	if policy.DeviceMode == "" {
		policy.DeviceMode = shared.DeviceModeInventoryOnly
	}
	policy.ManagedAccountEnabled = enabled == 1
	policy.FetchedAt = time.Now().UTC()
	return policy, nil
}

func (s *Store) SaveReport(ctx context.Context, token string, report shared.DeviceReport) error {
	device, err := s.AuthenticateAgent(ctx, token)
	if err != nil {
		return err
	}
	healthJSON, _ := json.Marshal(report.LastPolicyHealth)
	fingerprintsJSON, _ := json.Marshal(report.AuthorizedFingerprints)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `
		update device_reports
		set applied_revision = ?, user_present = ?, sudo_enabled = ?, audit_enabled = ?, authorized_fingerprints_json = ?, enforcement_succeeded = ?, error_message = ?, health_json = ?, updated_at = ?
		where device_id = ?`,
		report.AppliedRevision, boolInt(report.UserPresent), boolInt(report.SudoEnabled), boolInt(report.AuditEnabled),
		string(fingerprintsJSON), boolInt(report.EnforcementSucceeded), report.ErrorMessage, string(healthJSON), now, device.ID)
	if err != nil {
		return err
	}
	if err := s.saveDiscoverySnapshot(ctx, device.ID, report.Topology); err != nil {
		return err
	}
	if err := s.syncServiceInstances(ctx, device.ID, report.Topology.Workloads, report.Topology.UpdatedAt); err != nil {
		return err
	}
	return s.resolveTopology(ctx)
}

func (s *Store) saveDiscoverySnapshot(ctx context.Context, deviceID string, topology shared.TopologyDiscovery) error {
	wolJSON, _ := json.Marshal(topology.WakeOnLAN)
	workloadsJSON, _ := json.Marshal(topology.Workloads)
	childrenJSON, _ := json.Marshal(topology.ChildCandidates)
	warningsJSON, _ := json.Marshal(topology.Warnings)
	updatedAt := topology.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		update device_discovery_snapshots
		set device_type = ?, purpose = ?, platform_class = ?, wol_json = ?, workloads_json = ?, child_candidates_json = ?, warnings_json = ?, updated_at = ?
		where device_id = ?`,
		string(defaultDeviceType(topology.DeviceType)),
		string(defaultPurpose(topology.Purpose)),
		string(defaultPlatformClass(topology.PlatformClass)),
		string(wolJSON),
		string(workloadsJSON),
		string(childrenJSON),
		string(warningsJSON),
		updatedAt.Format(time.RFC3339),
		deviceID,
	)
	return err
}

func (s *Store) syncServiceInstances(ctx context.Context, deviceID string, workloads []shared.Workload, reportedAt time.Time) error {
	if reportedAt.IsZero() {
		reportedAt = time.Now().UTC()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	reported := reportedAt.UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	seen := map[string]struct{}{}
	for _, workload := range workloads {
		name := strings.TrimSpace(workload.Name)
		kind := workload.Kind
		if name == "" || kind == "" {
			continue
		}
		normalized := normalizeServiceName(name)
		key := string(kind) + "\x00" + normalized
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		state := strings.TrimSpace(workload.State)
		health := classifyServiceHealth(state, false)
		endpointsJSON, _ := json.Marshal(workload.Endpoints)
		var existingID int64
		var existingHealth string
		existingErr := tx.QueryRowContext(ctx, `
			select id, health from service_instances
			where device_id = ? and kind = ? and normalized_name = ?`,
			deviceID, string(kind), normalized).Scan(&existingID, &existingHealth)
		if _, err := tx.ExecContext(ctx, `
			insert into service_instances (
				device_id, normalized_name, name, kind, image, state, discovered_state, health, endpoints_json,
				first_seen_at, last_seen_at, missing_since, last_reported_at, created_at, updated_at
			)
			values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, null, ?, ?, ?)
			on conflict(device_id, kind, normalized_name) do update set
				name = excluded.name,
				image = excluded.image,
				state = excluded.state,
				discovered_state = excluded.discovered_state,
				health = excluded.health,
				endpoints_json = excluded.endpoints_json,
				last_seen_at = excluded.last_seen_at,
				missing_since = null,
				last_reported_at = excluded.last_reported_at,
				updated_at = excluded.updated_at`,
			deviceID, normalized, name, string(kind), strings.TrimSpace(workload.Image), state, state, string(health), string(endpointsJSON),
			reported, reported, reported, now, now); err != nil {
			return err
		}
		switch {
		case errors.Is(existingErr, sql.ErrNoRows):
			if err := insertServiceEventTx(ctx, tx, 0, deviceID, name, kind, "discovered", "", now); err != nil {
				return err
			}
		case existingErr != nil:
			return existingErr
		case existingHealth == string(shared.ServiceHealthMissing):
			if err := insertServiceEventTx(ctx, tx, existingID, deviceID, name, kind, "restored", "present in latest device report", now); err != nil {
				return err
			}
		}
	}

	rows, err := tx.QueryContext(ctx, `
		select id, normalized_name, kind, name, health
		from service_instances
		where device_id = ?`, deviceID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var normalized, kind, name, health string
		if err := rows.Scan(&id, &normalized, &kind, &name, &health); err != nil {
			_ = rows.Close()
			return err
		}
		key := kind + "\x00" + normalized
		if _, ok := seen[key]; ok || health == string(shared.ServiceHealthMissing) {
			continue
		}
		if err := insertServiceEventTx(ctx, tx, id, deviceID, name, shared.WorkloadKind(kind), "missing", "absent from latest device report", now); err != nil {
			_ = rows.Close()
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			update service_instances
			set health = ?, missing_since = coalesce(missing_since, ?), last_reported_at = ?, updated_at = ?
			where id = ?`, string(shared.ServiceHealthMissing), reported, reported, now, id); err != nil {
			_ = rows.Close()
			return err
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListServiceInstances(ctx context.Context) ([]ServiceInstanceRecord, error) {
	return s.queryServiceInstances(ctx, "", "")
}

func (s *Store) ListServiceInstancesForDevice(ctx context.Context, deviceID string) ([]ServiceInstanceRecord, error) {
	return s.queryServiceInstances(ctx, deviceID, "")
}

func (s *Store) SearchServiceInstances(ctx context.Context, query string) ([]ServiceInstanceRecord, error) {
	return s.queryServiceInstances(ctx, "", query)
}

func (s *Store) PruneMissingServiceInstances(ctx context.Context, deviceID, query string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	where := `where health = ?`
	args := []any{string(shared.ServiceHealthMissing)}
	if strings.TrimSpace(deviceID) != "" {
		where += ` and device_id = ?`
		args = append(args, deviceID)
	}
	if strings.TrimSpace(query) != "" {
		where += ` and (normalized_name like ? or lower(image) like ?)`
		pattern := "%" + normalizeServiceName(query) + "%"
		args = append(args, pattern, pattern)
	}
	rows, err := tx.QueryContext(ctx, `select id, device_id, name, kind from service_instances `+where, args...)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var ids []int64
	for rows.Next() {
		var id int64
		var eventDeviceID, name string
		var kind shared.WorkloadKind
		if err := rows.Scan(&id, &eventDeviceID, &name, &kind); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
		if err := insertServiceEventTx(ctx, tx, id, eventDeviceID, name, kind, "pruned", "removed missing service record", now); err != nil {
			_ = rows.Close()
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `delete from service_instances where id = ?`, id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

func insertServiceEventTx(ctx context.Context, tx *sql.Tx, serviceID int64, deviceID, name string, kind shared.WorkloadKind, action, details, createdAt string) error {
	var nullableID any
	if serviceID != 0 {
		nullableID = serviceID
	}
	_, err := tx.ExecContext(ctx, `
		insert into service_events (service_instance_id, device_id, service_name, service_kind, action, details, created_at)
		values (?, ?, ?, ?, ?, ?, ?)`, nullableID, deviceID, name, string(kind), action, details, createdAt)
	return err
}

func (s *Store) ListServiceEvents(ctx context.Context, limit int) ([]ServiceEventRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		select
			e.id, e.action, e.service_name, e.service_kind, e.details, e.created_at,
			d.id, d.name, d.bootstrap_token, d.agent_token, d.hostname, d.os_name, d.ips_json, d.agent_version, d.last_seen_at, d.created_at, d.updated_at
		from service_events e
		join devices d on d.id = e.device_id
		order by e.created_at desc, e.id desc
		limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServiceEventRecord
	for rows.Next() {
		event, err := scanServiceEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Store) queryServiceInstances(ctx context.Context, deviceID, query string) ([]ServiceInstanceRecord, error) {
	sqlText := `
		select
			s.id, s.normalized_name, s.name, s.kind, s.image, s.state, s.discovered_state, s.health, s.endpoints_json,
			s.first_seen_at, s.last_seen_at, s.missing_since, s.last_reported_at, s.created_at, s.updated_at,
			d.id, d.name, d.bootstrap_token, d.agent_token, d.hostname, d.os_name, d.ips_json, d.agent_version, d.last_seen_at, d.created_at, d.updated_at
		from service_instances s
		join devices d on d.id = s.device_id`
	var args []any
	var where []string
	if strings.TrimSpace(deviceID) != "" {
		where = append(where, "s.device_id = ?")
		args = append(args, deviceID)
	}
	if strings.TrimSpace(query) != "" {
		where = append(where, "(s.normalized_name like ? or lower(s.image) like ?)")
		pattern := "%" + normalizeServiceName(query) + "%"
		args = append(args, pattern, pattern)
	}
	if len(where) > 0 {
		sqlText += "\n\t\twhere " + strings.Join(where, " and ")
	}
	sqlText += "\n\t\torder by s.normalized_name asc, d.name asc, s.kind asc"
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServiceInstanceRecord
	for rows.Next() {
		record, err := scanServiceInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func normalizeServiceName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), " "))
}

func classifyServiceHealth(state string, missing bool) shared.ServiceHealth {
	if missing {
		return shared.ServiceHealthMissing
	}
	state = strings.ToLower(strings.TrimSpace(state))
	if state == "" {
		return shared.ServiceHealthHealthy
	}
	unhealthyMarkers := []string{"failed", "error", "unhealthy", "stopped", "exited", "dead", "down", "inactive"}
	for _, marker := range unhealthyMarkers {
		if strings.Contains(state, marker) {
			return shared.ServiceHealthUnhealthy
		}
	}
	healthyMarkers := []string{"running", "active", "up", "healthy"}
	for _, marker := range healthyMarkers {
		if strings.Contains(state, marker) {
			return shared.ServiceHealthHealthy
		}
	}
	return shared.ServiceHealthUnknown
}

func (s *Store) resolveTopology(ctx context.Context) error {
	records, err := s.ListDevices(ctx)
	if err != nil {
		return err
	}
	byID := make(map[string]DeviceRecord, len(records))
	for _, record := range records {
		byID[record.Device.ID] = record
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, child := range records {
		parentID, confidence, reason := inferParent(records, child)
		if child.Metadata.ParentOverrideState == shared.ParentOverrideManualDevice && child.Metadata.ParentOverrideDeviceID != nil {
			parentID = *child.Metadata.ParentOverrideDeviceID
			confidence = "manual"
			reason = "manual override"
		}
		if child.Metadata.ParentOverrideState == shared.ParentOverrideManualUnknown || child.Metadata.ParentOverrideState == shared.ParentOverrideManualNone {
			parentID = ""
			confidence = ""
			reason = ""
		}
		if _, err := s.db.ExecContext(ctx, `
			update relationship_candidates
			set parent_device_id = ?, confidence = ?, reason = ?, updated_at = ?
			where child_device_id = ?`, nullableString(parentID), confidence, reason, now, child.Device.ID); err != nil {
			return err
		}
	}
	_ = byID
	return nil
}

func inferParent(records []DeviceRecord, child DeviceRecord) (string, string, string) {
	if isStandaloneType(resolveEffectiveType(child)) {
		return "", "", ""
	}
	childHost := strings.ToLower(strings.TrimSpace(child.Device.Hostname))
	if childHost == "" || len(child.Device.IPs) == 0 {
		return "", "", ""
	}
	childIPs := make(map[string]struct{}, len(child.Device.IPs))
	for _, ip := range child.Device.IPs {
		childIPs[ip] = struct{}{}
	}
	for _, parent := range records {
		if parent.Device.ID == child.Device.ID {
			continue
		}
		for _, candidate := range parent.Discovery.ChildCandidates {
			if !namesMatch(candidate.Name, child) {
				continue
			}
			for _, ip := range candidate.IPs {
				if _, ok := childIPs[ip]; ok {
					return parent.Device.ID, "strong", "matched by hostname and ip"
				}
			}
		}
	}
	nameOnlyParentID := ""
	nameOnlyMatches := 0
	for _, parent := range records {
		if parent.Device.ID == child.Device.ID || defaultPurpose(parent.Discovery.Purpose) != shared.DevicePurposeProxmoxNode {
			continue
		}
		for _, candidate := range parent.Discovery.ChildCandidates {
			if !namesMatch(candidate.Name, child) {
				continue
			}
			if len(candidate.IPs) > 0 && !ipsOverlap(candidate.IPs, childIPs) {
				continue
			}
			nameOnlyMatches++
			nameOnlyParentID = parent.Device.ID
			break
		}
	}
	if nameOnlyMatches == 1 {
		return nameOnlyParentID, "medium", "unique proxmox child name match"
	}
	return "", "", ""
}

func ipsOverlap(values []string, wanted map[string]struct{}) bool {
	for _, value := range values {
		if _, ok := wanted[value]; ok {
			return true
		}
	}
	return false
}

func resolveEffectiveType(record DeviceRecord) shared.DeviceType {
	if record.Metadata.TypeOverride != nil && *record.Metadata.TypeOverride != "" {
		return *record.Metadata.TypeOverride
	}
	return defaultDeviceType(record.Discovery.DeviceType)
}

func defaultDeviceType(deviceType shared.DeviceType) shared.DeviceType {
	if deviceType == "" {
		return shared.DeviceTypeUnknown
	}
	return deviceType
}

func defaultPlatformClass(platform shared.PlatformClass) shared.PlatformClass {
	if platform == "" {
		return shared.PlatformClassUnknown
	}
	return platform
}

func defaultPurpose(purpose shared.DevicePurpose) shared.DevicePurpose {
	if purpose == "" {
		return shared.DevicePurposeUnknown
	}
	return purpose
}

func nullableString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func isStandaloneType(deviceType shared.DeviceType) bool {
	switch deviceType {
	case shared.DeviceTypeBareMetal, shared.DeviceTypeDockerHost, shared.DeviceTypeProxmoxNode:
		return true
	default:
		return false
	}
}

func namesMatch(candidate string, child DeviceRecord) bool {
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	if candidate == "" {
		return false
	}
	if strings.ToLower(child.Device.Hostname) == candidate {
		return true
	}
	for _, alias := range deviceAliases(child.Device.Name) {
		if strings.ToLower(alias) == candidate {
			return true
		}
	}
	return false
}

func randomToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func fingerprintAuthorizedKey(publicKey string) (string, error) {
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return "", fmt.Errorf("parse public key: %w", err)
	}
	return ssh.FingerprintSHA256(parsed), nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanDeviceRecord(row scanner) (DeviceRecord, error) {
	var (
		record                                        DeviceRecord
		deviceIPsJSON                                 string
		deviceLastSeen, deviceCreated                 sql.NullString
		deviceUpdated                                 string
		policyEnabled                                 sql.NullInt64
		policyMode                                    sql.NullString
		policyKeyID                                   sql.NullInt64
		policyRevision                                sql.NullInt64
		policyUpdated                                 sql.NullString
		keyName, keyValue, keyFP                      sql.NullString
		reportApplied                                 sql.NullInt64
		reportUser, reportSudo, reportAudit, reportOK sql.NullInt64
		reportAuthorizedJSON                          sql.NullString
		reportError                                   sql.NullString
		reportHealthJSON                              sql.NullString
		reportUpdated                                 sql.NullString
		policyDeviceMode                              sql.NullString
		metadataNote                                  sql.NullString
		metadataTypeOverride                          sql.NullString
		metadataPurposeOverride                       sql.NullString
		metadataParentOverrideID                      sql.NullString
		metadataParentOverrideState                   sql.NullString
		metadataUpdated                               sql.NullString
		discoveryDeviceType                           sql.NullString
		discoveryPurpose                              sql.NullString
		discoveryPlatform                             sql.NullString
		discoveryWOLJSON                              sql.NullString
		discoveryWorkloadsJSON                        sql.NullString
		discoveryChildrenJSON                         sql.NullString
		discoveryWarningsJSON                         sql.NullString
		discoveryUpdated                              sql.NullString
		relationshipParentID                          sql.NullString
		relationshipParentName                        sql.NullString
		overrideParentName                            sql.NullString
		relationshipConfidence                        sql.NullString
		relationshipReason                            sql.NullString
		relationshipUpdated                           sql.NullString
		updateOverride                                sql.NullString
		updateEffective                               sql.NullInt64
		updateAvailable                               sql.NullInt64
		updateServerVersion                           sql.NullString
		updateStatus                                  sql.NullString
		updateError                                   sql.NullString
		updateLastChecked                             sql.NullString
		updateLastAttempted                           sql.NullString
		updateGOOS                                    sql.NullString
		updateGOARCH                                  sql.NullString
	)
	if err := row.Scan(
		&record.Device.ID, &record.Device.Name, &record.Device.BootstrapToken, &record.Device.AgentToken,
		&record.Device.Hostname, &record.Device.OSName, &deviceIPsJSON, &record.Device.AgentVersion,
		&deviceLastSeen, &deviceCreated, &deviceUpdated,
		&policyDeviceMode, &policyEnabled, &policyMode, &policyKeyID, &policyRevision, &policyUpdated,
		&keyName, &keyValue, &keyFP,
		&reportApplied, &reportUser, &reportSudo, &reportAudit, &reportAuthorizedJSON, &reportOK, &reportError, &reportHealthJSON, &reportUpdated,
		&metadataNote, &metadataTypeOverride, &metadataPurposeOverride, &metadataParentOverrideID, &metadataParentOverrideState, &metadataUpdated,
		&discoveryDeviceType, &discoveryPurpose, &discoveryPlatform, &discoveryWOLJSON, &discoveryWorkloadsJSON, &discoveryChildrenJSON, &discoveryWarningsJSON, &discoveryUpdated,
		&relationshipParentID, &relationshipParentName, &overrideParentName, &relationshipConfidence, &relationshipReason, &relationshipUpdated,
		&updateOverride, &updateEffective, &updateAvailable, &updateServerVersion, &updateStatus, &updateError, &updateLastChecked, &updateLastAttempted, &updateGOOS, &updateGOARCH,
	); err != nil {
		return DeviceRecord{}, err
	}
	_ = json.Unmarshal([]byte(deviceIPsJSON), &record.Device.IPs)
	if deviceLastSeen.Valid {
		record.Device.LastSeenAt, _ = time.Parse(time.RFC3339, deviceLastSeen.String)
	}
	if deviceCreated.Valid {
		record.Device.CreatedAt, _ = time.Parse(time.RFC3339, deviceCreated.String)
	}
	record.Device.UpdatedAt, _ = time.Parse(time.RFC3339, deviceUpdated)
	record.Policy.DeviceID = record.Device.ID
	record.Policy.DeviceMode = shared.DeviceMode(policyDeviceMode.String)
	if record.Policy.DeviceMode == "" {
		record.Policy.DeviceMode = shared.DeviceModeInventoryOnly
	}
	record.Policy.ManagedAccountEnabled = policyEnabled.Int64 == 1
	record.Policy.AccessMode = shared.AccessMode(policyMode.String)
	if policyKeyID.Valid {
		id := policyKeyID.Int64
		record.Policy.SSHKeyID = &id
	}
	record.Policy.PolicyRevision = policyRevision.Int64
	record.Policy.AssignedKeyName = keyName.String
	record.Policy.AssignedKey = keyValue.String
	record.Policy.Fingerprint = keyFP.String
	if policyUpdated.Valid {
		record.Policy.UpdatedAt, _ = time.Parse(time.RFC3339, policyUpdated.String)
	}
	record.Report.DeviceID = record.Device.ID
	record.Report.AppliedRevision = reportApplied.Int64
	record.Report.UserPresent = reportUser.Int64 == 1
	record.Report.SudoEnabled = reportSudo.Int64 == 1
	record.Report.AuditEnabled = reportAudit.Int64 == 1
	record.Report.EnforcementSucceeded = reportOK.Int64 == 1
	record.Report.ErrorMessage = reportError.String
	if reportAuthorizedJSON.Valid {
		_ = json.Unmarshal([]byte(reportAuthorizedJSON.String), &record.Report.AuthorizedFingerprints)
	}
	if reportHealthJSON.Valid && reportHealthJSON.String != "" {
		_ = json.Unmarshal([]byte(reportHealthJSON.String), &record.Report.LastPolicyHealth)
	}
	if reportUpdated.Valid {
		record.Report.UpdatedAt, _ = time.Parse(time.RFC3339, reportUpdated.String)
	}
	record.Metadata.DeviceID = record.Device.ID
	record.Metadata.Note = metadataNote.String
	if metadataTypeOverride.Valid && metadataTypeOverride.String != "" {
		v := shared.DeviceType(metadataTypeOverride.String)
		record.Metadata.TypeOverride = &v
	}
	if metadataPurposeOverride.Valid && metadataPurposeOverride.String != "" {
		v := shared.DevicePurpose(metadataPurposeOverride.String)
		record.Metadata.PurposeOverride = &v
	}
	if metadataParentOverrideID.Valid && metadataParentOverrideID.String != "" {
		v := metadataParentOverrideID.String
		record.Metadata.ParentOverrideDeviceID = &v
	}
	record.Metadata.ParentOverrideState = shared.ParentOverrideState(metadataParentOverrideState.String)
	if record.Metadata.ParentOverrideState == "" {
		record.Metadata.ParentOverrideState = shared.ParentOverrideInherit
	}
	if metadataUpdated.Valid {
		record.Metadata.UpdatedAt, _ = time.Parse(time.RFC3339, metadataUpdated.String)
	}
	record.Discovery.DeviceID = record.Device.ID
	record.Discovery.DeviceType = defaultDeviceType(shared.DeviceType(discoveryDeviceType.String))
	record.Discovery.Purpose = defaultPurpose(shared.DevicePurpose(discoveryPurpose.String))
	record.Discovery.PlatformClass = defaultPlatformClass(shared.PlatformClass(discoveryPlatform.String))
	if discoveryWOLJSON.Valid && discoveryWOLJSON.String != "" {
		_ = json.Unmarshal([]byte(discoveryWOLJSON.String), &record.Discovery.WakeOnLAN)
	}
	if discoveryWorkloadsJSON.Valid && discoveryWorkloadsJSON.String != "" {
		_ = json.Unmarshal([]byte(discoveryWorkloadsJSON.String), &record.Discovery.Workloads)
	}
	if discoveryChildrenJSON.Valid && discoveryChildrenJSON.String != "" {
		_ = json.Unmarshal([]byte(discoveryChildrenJSON.String), &record.Discovery.ChildCandidates)
	}
	if discoveryWarningsJSON.Valid && discoveryWarningsJSON.String != "" {
		_ = json.Unmarshal([]byte(discoveryWarningsJSON.String), &record.Discovery.Warnings)
	}
	if discoveryUpdated.Valid {
		record.Discovery.UpdatedAt, _ = time.Parse(time.RFC3339, discoveryUpdated.String)
	}
	record.Resolved.DeviceID = record.Device.ID
	record.Resolved.PlatformClass = record.Discovery.PlatformClass
	record.Resolved.EffectiveDeviceType = resolveEffectiveType(record)
	if record.Metadata.PurposeOverride != nil && *record.Metadata.PurposeOverride != "" {
		record.Resolved.Purpose = *record.Metadata.PurposeOverride
		record.Resolved.PurposeSource = shared.TopologySourceManual
	} else {
		record.Resolved.Purpose = record.Discovery.Purpose
		if record.Resolved.Purpose != shared.DevicePurposeUnknown {
			record.Resolved.PurposeSource = shared.TopologySourceDiscovered
		} else {
			record.Resolved.PurposeSource = shared.TopologySourceUnknown
		}
	}
	if record.Metadata.TypeOverride != nil {
		record.Resolved.DeviceTypeSource = shared.TopologySourceManual
	} else if record.Discovery.DeviceType != shared.DeviceTypeUnknown {
		record.Resolved.DeviceTypeSource = shared.TopologySourceDiscovered
	} else {
		record.Resolved.DeviceTypeSource = shared.TopologySourceUnknown
	}
	switch record.Metadata.ParentOverrideState {
	case shared.ParentOverrideManualDevice:
		record.Resolved.ParentSource = shared.TopologySourceManual
		if record.Metadata.ParentOverrideDeviceID != nil {
			record.Resolved.ParentDeviceID = *record.Metadata.ParentOverrideDeviceID
		}
		record.Resolved.ParentName = overrideParentName.String
	case shared.ParentOverrideManualUnknown:
		record.Resolved.ParentSource = shared.TopologySourceManual
		record.Resolved.ParentState = shared.ParentStateUnknown
	case shared.ParentOverrideManualNone:
		record.Resolved.ParentSource = shared.TopologySourceManual
		record.Resolved.ParentState = shared.ParentStateNone
	default:
		if relationshipParentID.Valid && relationshipParentID.String != "" {
			record.Resolved.ParentDeviceID = relationshipParentID.String
			record.Resolved.ParentName = relationshipParentName.String
			record.Resolved.ParentState = shared.ParentStateLinked
			record.Resolved.ParentSource = shared.TopologySourceInferred
			record.Resolved.MatchConfidence = relationshipConfidence.String
			record.Resolved.MatchReason = relationshipReason.String
		}
	}
	if record.Resolved.ParentState == "" {
		if record.Resolved.ParentDeviceID != "" {
			record.Resolved.ParentState = shared.ParentStateLinked
		} else if isStandaloneType(record.Resolved.EffectiveDeviceType) {
			record.Resolved.ParentState = shared.ParentStateNone
			if record.Resolved.ParentSource == "" {
				record.Resolved.ParentSource = shared.TopologySourceNone
			}
		} else {
			record.Resolved.ParentState = shared.ParentStateUnknown
			if record.Resolved.ParentSource == "" {
				record.Resolved.ParentSource = shared.TopologySourceUnknown
			}
		}
	}
	if relationshipUpdated.Valid {
		record.Resolved.UpdatedAt, _ = time.Parse(time.RFC3339, relationshipUpdated.String)
	}
	record.Update.DeviceID = record.Device.ID
	record.Update.Override = shared.AgentAutoUpdateOverride(updateOverride.String)
	if record.Update.Override == "" {
		record.Update.Override = shared.AgentAutoUpdateInherit
	}
	record.Update.EffectiveEnabled = updateEffective.Int64 == 1
	record.Update.UpdateAvailable = updateAvailable.Int64 == 1
	record.Update.ServerAgentVersion = updateServerVersion.String
	record.Update.Status = shared.AgentUpdateStatus(updateStatus.String)
	if record.Update.Status == "" {
		record.Update.Status = shared.AgentUpdateStatusIdle
	}
	record.Update.Error = updateError.String
	if updateLastChecked.Valid {
		record.Update.LastCheckedAt, _ = time.Parse(time.RFC3339, updateLastChecked.String)
	}
	if updateLastAttempted.Valid {
		record.Update.LastAttemptedAt, _ = time.Parse(time.RFC3339, updateLastAttempted.String)
	}
	record.Update.ReportedGOOS = updateGOOS.String
	record.Update.ReportedGOARCH = updateGOARCH.String
	return record, nil
}

func scanDevice(row scanner) (shared.Device, error) {
	var device shared.Device
	var ipsJSON string
	var lastSeen, created, updated sql.NullString
	if err := row.Scan(&device.ID, &device.Name, &device.BootstrapToken, &device.AgentToken, &device.Hostname, &device.OSName, &ipsJSON, &device.AgentVersion, &lastSeen, &created, &updated); err != nil {
		return shared.Device{}, err
	}
	_ = json.Unmarshal([]byte(ipsJSON), &device.IPs)
	if lastSeen.Valid {
		device.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeen.String)
	}
	if created.Valid {
		device.CreatedAt, _ = time.Parse(time.RFC3339, created.String)
	}
	if updated.Valid {
		device.UpdatedAt, _ = time.Parse(time.RFC3339, updated.String)
	}
	return device, nil
}

func scanServiceInstance(row scanner) (ServiceInstanceRecord, error) {
	var (
		record                                                                                 ServiceInstanceRecord
		endpointsJSON                                                                          string
		firstSeen, serviceLastSeen, missingSince, lastReported, serviceCreated, serviceUpdated sql.NullString
		deviceIPsJSON                                                                          string
		deviceLastSeen, deviceCreated, deviceUpdated                                           sql.NullString
	)
	if err := row.Scan(
		&record.ID, &record.NormalizedName, &record.Name, &record.Kind, &record.Image, &record.State, &record.DiscoveredState, &record.Health, &endpointsJSON,
		&firstSeen, &serviceLastSeen, &missingSince, &lastReported, &serviceCreated, &serviceUpdated,
		&record.Device.ID, &record.Device.Name, &record.Device.BootstrapToken, &record.Device.AgentToken,
		&record.Device.Hostname, &record.Device.OSName, &deviceIPsJSON, &record.Device.AgentVersion,
		&deviceLastSeen, &deviceCreated, &deviceUpdated,
	); err != nil {
		return ServiceInstanceRecord{}, err
	}
	_ = json.Unmarshal([]byte(endpointsJSON), &record.Endpoints)
	_ = json.Unmarshal([]byte(deviceIPsJSON), &record.Device.IPs)
	if firstSeen.Valid {
		record.FirstSeenAt, _ = time.Parse(time.RFC3339, firstSeen.String)
	}
	if serviceLastSeen.Valid {
		record.LastSeenAt, _ = time.Parse(time.RFC3339, serviceLastSeen.String)
	}
	if missingSince.Valid {
		record.MissingSince, _ = time.Parse(time.RFC3339, missingSince.String)
	}
	if lastReported.Valid {
		record.LastReportedAt, _ = time.Parse(time.RFC3339, lastReported.String)
	}
	if serviceCreated.Valid {
		record.CreatedAt, _ = time.Parse(time.RFC3339, serviceCreated.String)
	}
	if serviceUpdated.Valid {
		record.UpdatedAt, _ = time.Parse(time.RFC3339, serviceUpdated.String)
	}
	if deviceLastSeen.Valid {
		record.Device.LastSeenAt, _ = time.Parse(time.RFC3339, deviceLastSeen.String)
	}
	if deviceCreated.Valid {
		record.Device.CreatedAt, _ = time.Parse(time.RFC3339, deviceCreated.String)
	}
	if deviceUpdated.Valid {
		record.Device.UpdatedAt, _ = time.Parse(time.RFC3339, deviceUpdated.String)
	}
	if record.Health == "" {
		record.Health = shared.ServiceHealthUnknown
	}
	return record, nil
}

func scanServiceEvent(row scanner) (ServiceEventRecord, error) {
	var (
		event                                        ServiceEventRecord
		created                                      string
		deviceIPsJSON                                string
		deviceLastSeen, deviceCreated, deviceUpdated sql.NullString
	)
	if err := row.Scan(
		&event.ID, &event.Action, &event.ServiceName, &event.ServiceKind, &event.Details, &created,
		&event.Device.ID, &event.Device.Name, &event.Device.BootstrapToken, &event.Device.AgentToken,
		&event.Device.Hostname, &event.Device.OSName, &deviceIPsJSON, &event.Device.AgentVersion,
		&deviceLastSeen, &deviceCreated, &deviceUpdated,
	); err != nil {
		return ServiceEventRecord{}, err
	}
	event.CreatedAt, _ = time.Parse(time.RFC3339, created)
	_ = json.Unmarshal([]byte(deviceIPsJSON), &event.Device.IPs)
	if deviceLastSeen.Valid {
		event.Device.LastSeenAt, _ = time.Parse(time.RFC3339, deviceLastSeen.String)
	}
	if deviceCreated.Valid {
		event.Device.CreatedAt, _ = time.Parse(time.RFC3339, deviceCreated.String)
	}
	if deviceUpdated.Valid {
		event.Device.UpdatedAt, _ = time.Parse(time.RFC3339, deviceUpdated.String)
	}
	return event, nil
}

func scanTopologyNode(row scanner) (ManualTopologyNode, error) {
	var node ManualTopologyNode
	var createdAt, updatedAt string
	if err := row.Scan(&node.ID, &node.Name, &node.Kind, &node.Note, &createdAt, &updatedAt); err != nil {
		return ManualTopologyNode{}, err
	}
	node.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	node.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return node, nil
}

func scanTopologyLink(row scanner) (ManualTopologyLink, error) {
	var link ManualTopologyLink
	var createdAt, updatedAt string
	if err := row.Scan(&link.ID, &link.FromKind, &link.FromID, &link.ToKind, &link.ToID, &link.Label, &link.Source, &createdAt, &updatedAt); err != nil {
		return ManualTopologyLink{}, err
	}
	link.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	link.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return link, nil
}

func validManualTopologyNodeKind(kind shared.TopologyNodeKind) bool {
	switch kind {
	case shared.TopologyNodeKindInternet,
		shared.TopologyNodeKindRouter,
		shared.TopologyNodeKindSwitch,
		shared.TopologyNodeKindAccessPoint,
		shared.TopologyNodeKindPatchPanel,
		shared.TopologyNodeKindOther:
		return true
	default:
		return false
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func effectiveAgentAutoUpdateStore(globalEnabled bool, override shared.AgentAutoUpdateOverride) bool {
	switch override {
	case shared.AgentAutoUpdateEnabled:
		return true
	case shared.AgentAutoUpdateDisabled:
		return false
	default:
		return globalEnabled
	}
}

func isUniqueConstraint(err error, name string) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, strings.ToLower(name)) {
		return true
	}
	if strings.Contains(msg, "unique constraint failed") && strings.Contains(msg, "devices.name") {
		return name == "devices_name_unique"
	}
	return strings.Contains(msg, "unique constraint failed") && strings.Contains(msg, "topology_links.")
}
