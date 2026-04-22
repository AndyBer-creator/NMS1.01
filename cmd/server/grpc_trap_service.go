package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"NMS1/internal/config"
	"NMS1/internal/grpcapi"
	"NMS1/internal/repository"

	"go.uber.org/zap"
)

type trapIngestService struct {
	repo              *repository.TrapsRepo
	log               *zap.Logger
	suppressionWindow time.Duration
}

func (s *trapIngestService) IngestTrap(ctx context.Context, req *grpcapi.TrapIngestRequest) (*grpcapi.TrapIngestResponse, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("trap repo is not configured")
	}
	deviceIP := strings.TrimSpace(req.DeviceIP)
	if deviceIP == "" {
		return nil, fmt.Errorf("device_ip is required")
	}
	oid := strings.TrimSpace(req.OID)
	if oid == "" {
		oid = "unknown"
	}
	if req.TrapVars == nil {
		req.TrapVars = map[string]string{}
	}
	if err := s.repo.Insert(ctx, deviceIP, oid, req.Uptime, req.TrapVars, false); err != nil {
		return nil, err
	}
	if err := s.repo.CreateOrTouchOpenTrapIncident(ctx, deviceIP, oid, req.TrapVars, s.suppressionWindow); err != nil {
		s.log.Warn("grpc trap incident correlation failed",
			zap.String("from", deviceIP),
			zap.String("oid", oid),
			zap.Error(err))
	}
	return &grpcapi.TrapIngestResponse{Status: "ok"}, nil
}

func trapIncidentSuppressionWindow() time.Duration {
	raw := strings.TrimSpace(config.EnvOrFile("NMS_TRAP_INCIDENT_SUPPRESSION_WINDOW"))
	if raw == "" {
		return 10 * time.Minute
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 10 * time.Minute
	}
	return d
}
