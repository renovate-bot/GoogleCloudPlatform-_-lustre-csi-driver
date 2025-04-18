/*
Copyright 2025 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package lustre

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/compute/metadata"
	"github.com/GoogleCloudPlatform/lustre-csi-driver/pkg/cloud_provider/auth"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
	"gopkg.in/gcfg.v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

type Cloud struct {
	LustreService Service
	Project       string
	Zone          string
}

type ConfigFile struct {
	Global ConfigGlobal `gcfg:"global"`
}

type ConfigGlobal struct {
	TokenURL  string `gcfg:"token-url"`
	TokenBody string `gcfg:"token-body"`
	ProjectID string `gcfg:"project-id"`
	Zone      string `gcfg:"zone"`
}

func NewCloud(ctx context.Context, configPath, version, endpoint string) (*Cloud, error) {
	configFile, err := maybeReadConfig(configPath)
	if err != nil {
		return nil, err
	}

	tokenSource, err := generateTokenSource(ctx, configFile)
	if err != nil {
		return nil, err
	}

	client, err := newOauthClient(ctx, tokenSource)
	if err != nil {
		return nil, err
	}

	service, err := NewLustreService(ctx, client, version, endpoint)
	if err != nil {
		return nil, err
	}

	project, zone, err := getProjectAndZone(ctx, configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize project information: %w", err)
	}

	return &Cloud{
		LustreService: service,
		Project:       project,
		Zone:          zone,
	}, nil
}

func newOauthClient(ctx context.Context, tokenSource oauth2.TokenSource) (*http.Client, error) {
	if err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 30*time.Second, true, func(context.Context) (bool, error) {
		if _, err := tokenSource.Token(); err != nil {
			klog.Errorf("error fetching initial token: %v", err.Error())

			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	return oauth2.NewClient(ctx, tokenSource), nil
}

func maybeReadConfig(configPath string) (*ConfigFile, error) {
	if configPath == "" {
		return nil, nil
	}

	reader, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't open cloud provider configuration at %s: %w", configPath, err)
	}
	defer reader.Close()

	cfg := &ConfigFile{}
	if err := gcfg.FatalOnly(gcfg.ReadInto(cfg, reader)); err != nil {
		return nil, fmt.Errorf("couldn't read cloud provider configuration at %s: %w", configPath, err)
	}
	klog.Infof("Config file read %#v", cfg)

	return cfg, nil
}

func generateTokenSource(ctx context.Context, configFile *ConfigFile) (oauth2.TokenSource, error) {
	// If configFile.Global.TokenURL is defined use AltTokenSource
	if configFile != nil && configFile.Global.TokenURL != "" && configFile.Global.TokenURL != "nil" {
		tokenSource := auth.NewAltTokenSource(ctx, configFile.Global.TokenURL, configFile.Global.TokenBody)
		klog.Infof("Using AltTokenSource %#v", tokenSource)

		return tokenSource, nil
	}

	// Use DefaultTokenSource
	tokenSource, err := google.DefaultTokenSource(
		ctx,
		compute.CloudPlatformScope)

	// DefaultTokenSource relies on GOOGLE_APPLICATION_CREDENTIALS env var being set.
	if gac, ok := os.LookupEnv("GOOGLE_APPLICATION_CREDENTIALS"); ok {
		klog.Infof("GOOGLE_APPLICATION_CREDENTIALS env var set %v", gac)
	} else {
		klog.Warningf("GOOGLE_APPLICATION_CREDENTIALS env var not set")
	}
	klog.Infof("Using DefaultTokenSource %#v", tokenSource)

	return tokenSource, err
}

// getProjectAndZone fetches project and zone information from either the configFile or metadata server.
// The lookup is first done in configFile contents and then metadata server.
func getProjectAndZone(ctx context.Context, config *ConfigFile) (string, string, error) {
	var err error
	var zone string
	if config == nil || config.Global.Zone == "" {
		zone, err = metadata.ZoneWithContext(ctx)
		if err != nil {
			return "", "", err
		}
		klog.Infof("Using GCP zone from the Metadata server: %q", zone)
	} else {
		zone = config.Global.Zone
		klog.Infof("Using GCP zone from the local GCE cloud provider config file: %q", zone)
	}

	var projectID string
	if config == nil || config.Global.ProjectID == "" {
		// Project ID is not available from the local GCE cloud provider config file.
		// This could happen if the driver is not running in the master VM.
		// Defaulting to project ID from the Metadata server.
		projectID, err = metadata.ProjectIDWithContext(ctx)
		if err != nil {
			return "", "", err
		}
		klog.Infof("Using GCP project ID %q from the Metadata server", projectID)
	} else {
		projectID = config.Global.ProjectID
		klog.Infof("Using GCP project ID %q from the local GCE cloud provider config file: %#v", projectID, config)
	}

	return projectID, zone, nil
}
