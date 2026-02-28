package cluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jfoltran/migrator/internal/daemon"
)

type NodeRole string

const (
	RolePrimary NodeRole = "primary"
	RoleReplica NodeRole = "replica"
	RoleStandby NodeRole = "standby"
)

type Node struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Host     string   `json:"host"`
	Port     uint16   `json:"port"`
	Role     NodeRole `json:"role"`
	AgentURL string   `json:"agent_url,omitempty"`
}

type Cluster struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Nodes     []Node    `json:"nodes"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data storeData
}

type storeData struct {
	Clusters []Cluster `json:"clusters"`
}

func NewStore() (*Store, error) {
	dir, err := daemon.DataDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "clusters.json")
	s := &Store{path: path}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load cluster store: %w", err)
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.data)
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) List() []Cluster {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Cluster, len(s.data.Clusters))
	copy(out, s.data.Clusters)
	return out
}

func (s *Store) Get(id string) (Cluster, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.data.Clusters {
		if c.ID == id {
			return c, true
		}
	}
	return Cluster{}, false
}

func (s *Store) Add(c Cluster) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.data.Clusters {
		if existing.ID == c.ID {
			return fmt.Errorf("cluster %q already exists", c.ID)
		}
	}

	now := time.Now().UTC()
	c.CreatedAt = now
	c.UpdatedAt = now
	s.data.Clusters = append(s.data.Clusters, c)
	return s.save()
}

func (s *Store) Update(c Cluster) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, existing := range s.data.Clusters {
		if existing.ID == c.ID {
			c.CreatedAt = existing.CreatedAt
			c.UpdatedAt = time.Now().UTC()
			s.data.Clusters[i] = c
			return s.save()
		}
	}
	return fmt.Errorf("cluster %q not found", c.ID)
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, c := range s.data.Clusters {
		if c.ID == id {
			s.data.Clusters = append(s.data.Clusters[:i], s.data.Clusters[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("cluster %q not found", id)
}

func (s *Store) AddNode(clusterID string, n Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, c := range s.data.Clusters {
		if c.ID == clusterID {
			for _, existing := range c.Nodes {
				if existing.ID == n.ID {
					return fmt.Errorf("node %q already exists in cluster %q", n.ID, clusterID)
				}
			}
			s.data.Clusters[i].Nodes = append(s.data.Clusters[i].Nodes, n)
			s.data.Clusters[i].UpdatedAt = time.Now().UTC()
			return s.save()
		}
	}
	return fmt.Errorf("cluster %q not found", clusterID)
}

func (s *Store) RemoveNode(clusterID, nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, c := range s.data.Clusters {
		if c.ID == clusterID {
			for j, n := range c.Nodes {
				if n.ID == nodeID {
					s.data.Clusters[i].Nodes = append(c.Nodes[:j], c.Nodes[j+1:]...)
					s.data.Clusters[i].UpdatedAt = time.Now().UTC()
					return s.save()
				}
			}
			return fmt.Errorf("node %q not found in cluster %q", nodeID, clusterID)
		}
	}
	return fmt.Errorf("cluster %q not found", clusterID)
}

func ValidateCluster(c Cluster) error {
	var errs []error
	if c.ID == "" {
		errs = append(errs, errors.New("cluster id is required"))
	}
	if c.Name == "" {
		errs = append(errs, errors.New("cluster name is required"))
	}
	if len(c.Nodes) == 0 {
		errs = append(errs, errors.New("at least one node is required"))
	}
	for _, n := range c.Nodes {
		if n.ID == "" {
			errs = append(errs, errors.New("node id is required"))
		}
		if n.Host == "" {
			errs = append(errs, errors.New("node host is required"))
		}
		if n.Port == 0 {
			errs = append(errs, fmt.Errorf("node %q port is required", n.ID))
		}
	}
	return errors.Join(errs...)
}
