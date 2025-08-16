package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
	
	"azure-openai-proxy/internal/config"
	
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SQLiteStore implements ConfigStore using SQLite
type SQLiteStore struct {
	db *gorm.DB
}

// ConfigRecord represents a configuration record in the database
type ConfigRecord struct {
	ID        uint      `gorm:"primaryKey"`
	Name      string    `gorm:"uniqueIndex;not null"`
	Type      string    `gorm:"not null"` // "app" or "instance"
	Data      string    `gorm:"type:text;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewSQLiteStore creates a new SQLite-based config store
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite: %w", err)
	}
	
	// Auto-migrate the schema
	err = db.AutoMigrate(&ConfigRecord{})
	if err != nil {
		return nil, fmt.Errorf("failed to migrate database schema: %w", err)
	}
	
	return &SQLiteStore{db: db}, nil
}

// SaveConfig saves the application configuration
func (s *SQLiteStore) SaveConfig(ctx context.Context, config *config.AppConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal app config: %w", err)
	}
	
	record := ConfigRecord{
		Name: "app",
		Type: "app",
		Data: string(data),
	}
	
	err = s.db.WithContext(ctx).
		Where("name = ? AND type = ?", "app", "app").
		FirstOrCreate(&record).Error
	
	if err != nil {
		return fmt.Errorf("failed to save app config: %w", err)
	}
	
	// Update the data if record already existed
	if record.ID != 0 {
		err = s.db.WithContext(ctx).
			Model(&record).
			Update("data", string(data)).Error
		if err != nil {
			return fmt.Errorf("failed to update app config: %w", err)
		}
	}
	
	return nil
}

// LoadConfig loads the application configuration
func (s *SQLiteStore) LoadConfig(ctx context.Context) (*config.AppConfig, error) {
	var record ConfigRecord
	
	err := s.db.WithContext(ctx).
		Where("name = ? AND type = ?", "app", "app").
		First(&record).Error
	
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("app config not found")
		}
		return nil, fmt.Errorf("failed to load app config: %w", err)
	}
	
	var appConfig config.AppConfig
	if err := json.Unmarshal([]byte(record.Data), &appConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal app config: %w", err)
	}
	
	return &appConfig, nil
}

// SaveInstanceConfig saves a specific instance configuration
func (s *SQLiteStore) SaveInstanceConfig(ctx context.Context, instance *config.InstanceConfig) error {
	data, err := json.Marshal(instance)
	if err != nil {
		return fmt.Errorf("failed to marshal instance config: %w", err)
	}
	
	record := ConfigRecord{
		Name: instance.Name,
		Type: "instance",
		Data: string(data),
	}
	
	err = s.db.WithContext(ctx).
		Where("name = ? AND type = ?", instance.Name, "instance").
		FirstOrCreate(&record).Error
	
	if err != nil {
		return fmt.Errorf("failed to save instance config: %w", err)
	}
	
	// Update the data if record already existed
	if record.ID != 0 {
		err = s.db.WithContext(ctx).
			Model(&record).
			Update("data", string(data)).Error
		if err != nil {
			return fmt.Errorf("failed to update instance config: %w", err)
		}
	}
	
	return nil
}

// LoadInstanceConfig loads a specific instance configuration
func (s *SQLiteStore) LoadInstanceConfig(ctx context.Context, name string) (*config.InstanceConfig, error) {
	var record ConfigRecord
	
	err := s.db.WithContext(ctx).
		Where("name = ? AND type = ?", name, "instance").
		First(&record).Error
	
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("instance config not found: %s", name)
		}
		return nil, fmt.Errorf("failed to load instance config: %w", err)
	}
	
	var instanceConfig config.InstanceConfig
	if err := json.Unmarshal([]byte(record.Data), &instanceConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal instance config: %w", err)
	}
	
	return &instanceConfig, nil
}

// DeleteInstanceConfig deletes a specific instance configuration
func (s *SQLiteStore) DeleteInstanceConfig(ctx context.Context, name string) error {
	err := s.db.WithContext(ctx).
		Where("name = ? AND type = ?", name, "instance").
		Delete(&ConfigRecord{}).Error
	
	if err != nil {
		return fmt.Errorf("failed to delete instance config: %w", err)
	}
	
	return nil
}

// ListInstanceConfigs returns all instance configuration names
func (s *SQLiteStore) ListInstanceConfigs(ctx context.Context) ([]string, error) {
	var records []ConfigRecord
	
	err := s.db.WithContext(ctx).
		Select("name").
		Where("type = ?", "instance").
		Find(&records).Error
	
	if err != nil {
		return nil, fmt.Errorf("failed to list instance configs: %w", err)
	}
	
	names := make([]string, len(records))
	for i, record := range records {
		names[i] = record.Name
	}
	
	return names, nil
}

// Close closes the SQLite connection
func (s *SQLiteStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// GetStats returns database statistics
func (s *SQLiteStore) GetStats(ctx context.Context) (map[string]interface{}, error) {
	var appCount, instanceCount int64
	
	err := s.db.WithContext(ctx).
		Model(&ConfigRecord{}).
		Where("type = ?", "app").
		Count(&appCount).Error
	if err != nil {
		return nil, fmt.Errorf("failed to count app configs: %w", err)
	}
	
	err = s.db.WithContext(ctx).
		Model(&ConfigRecord{}).
		Where("type = ?", "instance").
		Count(&instanceCount).Error
	if err != nil {
		return nil, fmt.Errorf("failed to count instance configs: %w", err)
	}
	
	// Get database size
	var dbSize sql.NullString
	err = s.db.WithContext(ctx).Raw("SELECT page_count * page_size as size FROM pragma_page_count(), pragma_page_size()").Scan(&dbSize).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get database size: %w", err)
	}
	
	stats := map[string]interface{}{
		"app_configs":      appCount,
		"instance_configs": instanceCount,
		"database_size":    dbSize.String,
	}
	
	return stats, nil
}