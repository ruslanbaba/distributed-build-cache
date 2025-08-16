package security

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"
)

// ZeroTrustSecurityManager implements zero-trust security principles
type ZeroTrustSecurityManager struct {
	logger          *zap.Logger
	jwtSecret       []byte
	certManager     *CertificateManager
	policyEngine    *PolicyEngine
	auditLogger     *AuditLogger
	threatDetector  *ThreatDetector
	
	// Security policies
	enableMTLS      bool
	requireAuth     bool
	enableRBAC      bool
	enableMFA       bool
}

// CertificateManager handles dynamic certificate management
type CertificateManager struct {
	certStore    map[string]*tls.Certificate
	caPool       *x509.CertPool
	autoRotate   bool
	rotationDays int
	logger       *zap.Logger
}

// PolicyEngine enforces security policies
type PolicyEngine struct {
	policies      []SecurityPolicy
	ruleEngine    *RuleEngine
	contextCache  map[string]*SecurityContext
	logger        *zap.Logger
}

// SecurityPolicy defines access control policies
type SecurityPolicy struct {
	ID          string            `yaml:"id"`
	Name        string            `yaml:"name"`
	Rules       []SecurityRule    `yaml:"rules"`
	Actions     []string          `yaml:"actions"`
	Resources   []string          `yaml:"resources"`
	Conditions  map[string]string `yaml:"conditions"`
	Priority    int               `yaml:"priority"`
	Enabled     bool              `yaml:"enabled"`
}

// SecurityRule defines individual security rules
type SecurityRule struct {
	Type        string            `yaml:"type"` // allow, deny, require_mfa
	Subjects    []string          `yaml:"subjects"`
	Actions     []string          `yaml:"actions"`
	Resources   []string          `yaml:"resources"`
	Conditions  map[string]string `yaml:"conditions"`
	TimeWindows []TimeWindow      `yaml:"timeWindows"`
}

// TimeWindow defines time-based access controls
type TimeWindow struct {
	StartTime string `yaml:"startTime"` // HH:MM format
	EndTime   string `yaml:"endTime"`   // HH:MM format
	Days      []string `yaml:"days"`    // monday, tuesday, etc.
	Timezone  string `yaml:"timezone"`
}

// ThreatDetector identifies security threats in real-time
type ThreatDetector struct {
	anomalyDetector *AnomalyDetector
	patterns        []ThreatPattern
	alertManager    *AlertManager
	quarantine      *QuarantineManager
	logger          *zap.Logger
}

// NewZeroTrustSecurityManager creates a new zero-trust security manager
func NewZeroTrustSecurityManager(config SecurityConfig, logger *zap.Logger) (*ZeroTrustSecurityManager, error) {
	certManager, err := NewCertificateManager(config.CertConfig, logger)
	if err != nil {
		return nil, err
	}

	policyEngine, err := NewPolicyEngine(config.PolicyConfig, logger)
	if err != nil {
		return nil, err
	}

	auditLogger := NewAuditLogger(config.AuditConfig, logger)
	threatDetector := NewThreatDetector(config.ThreatConfig, logger)

	return &ZeroTrustSecurityManager{
		logger:         logger,
		jwtSecret:      []byte(config.JWTSecret),
		certManager:    certManager,
		policyEngine:   policyEngine,
		auditLogger:    auditLogger,
		threatDetector: threatDetector,
		enableMTLS:     config.EnableMTLS,
		requireAuth:    config.RequireAuth,
		enableRBAC:     config.EnableRBAC,
		enableMFA:      config.EnableMFA,
	}, nil
}

// AuthenticateRequest performs comprehensive request authentication
func (zsm *ZeroTrustSecurityManager) AuthenticateRequest(ctx context.Context, req *http.Request) (*SecurityContext, error) {
	requestID := uuid.New().String()
	startTime := time.Now()

	// Extract metadata
	md, _ := metadata.FromIncomingContext(ctx)
	
	// Log request for audit
	zsm.auditLogger.LogRequest(AuditEvent{
		RequestID:   requestID,
		Timestamp:   startTime,
		Method:      req.Method,
		URL:         req.URL.String(),
		ClientIP:    getClientIP(req),
		UserAgent:   req.UserAgent(),
		Headers:     req.Header,
		Metadata:    md,
	})

	// Step 1: Certificate-based authentication (mTLS)
	if zsm.enableMTLS {
		if err := zsm.validateClientCertificate(req); err != nil {
			zsm.auditLogger.LogSecurityEvent(SecurityEvent{
				Type:      "auth_failure",
				Reason:    "invalid_certificate",
				RequestID: requestID,
				ClientIP:  getClientIP(req),
			})
			return nil, fmt.Errorf("certificate validation failed: %w", err)
		}
	}

	// Step 2: JWT token validation
	var claims *jwt.RegisteredClaims
	if zsm.requireAuth {
		token := extractToken(req)
		if token == "" {
			return nil, fmt.Errorf("authentication required")
		}

		var err error
		claims, err = zsm.validateJWT(token)
		if err != nil {
			zsm.auditLogger.LogSecurityEvent(SecurityEvent{
				Type:      "auth_failure",
				Reason:    "invalid_token",
				RequestID: requestID,
				ClientIP:  getClientIP(req),
			})
			return nil, fmt.Errorf("token validation failed: %w", err)
		}
	}

	// Step 3: Threat detection
	threat := zsm.threatDetector.AnalyzeRequest(ctx, req, claims)
	if threat.RiskScore > 0.8 {
		zsm.threatDetector.quarantine.QuarantineClient(getClientIP(req))
		return nil, fmt.Errorf("request blocked due to security threat")
	}

	// Step 4: Build security context
	securityContext := &SecurityContext{
		RequestID:   requestID,
		UserID:      getUserID(claims),
		ClientIP:    getClientIP(req),
		Permissions: zsm.getPermissions(claims),
		ThreatScore: threat.RiskScore,
		Authenticated: zsm.requireAuth,
		StartTime:   startTime,
	}

	// Step 5: Multi-factor authentication check
	if zsm.enableMFA && zsm.requiresMFA(securityContext) {
		if !zsm.validateMFA(req, securityContext) {
			return nil, fmt.Errorf("multi-factor authentication required")
		}
	}

	return securityContext, nil
}

// AuthorizeRequest performs fine-grained authorization
func (zsm *ZeroTrustSecurityManager) AuthorizeRequest(ctx context.Context, secCtx *SecurityContext, resource, action string) error {
	if !zsm.enableRBAC {
		return nil // Authorization disabled
	}

	// Evaluate policies
	decision, err := zsm.policyEngine.Evaluate(PolicyRequest{
		Subject:  secCtx.UserID,
		Action:   action,
		Resource: resource,
		Context:  secCtx,
	})

	if err != nil {
		zsm.auditLogger.LogSecurityEvent(SecurityEvent{
			Type:      "authz_error",
			RequestID: secCtx.RequestID,
			UserID:    secCtx.UserID,
			Resource:  resource,
			Action:    action,
			Error:     err.Error(),
		})
		return fmt.Errorf("authorization evaluation failed: %w", err)
	}

	if decision.Decision != "allow" {
		zsm.auditLogger.LogSecurityEvent(SecurityEvent{
			Type:      "authz_denied",
			RequestID: secCtx.RequestID,
			UserID:    secCtx.UserID,
			Resource:  resource,
			Action:    action,
			Reason:    decision.Reason,
		})
		return fmt.Errorf("access denied: %s", decision.Reason)
	}

	zsm.auditLogger.LogSecurityEvent(SecurityEvent{
		Type:      "authz_granted",
		RequestID: secCtx.RequestID,
		UserID:    secCtx.UserID,
		Resource:  resource,
		Action:    action,
	})

	return nil
}

// validateJWT validates JWT tokens with comprehensive checks
func (zsm *ZeroTrustSecurityManager) validateJWT(tokenString string) (*jwt.RegisteredClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return zsm.jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Additional validation
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("token expired")
	}

	if claims.NotBefore != nil && claims.NotBefore.After(time.Now()) {
		return nil, fmt.Errorf("token not yet valid")
	}

	return claims, nil
}

// SecurityContext represents the security context for a request
type SecurityContext struct {
	RequestID     string
	UserID        string
	ClientIP      string
	Permissions   []string
	ThreatScore   float64
	Authenticated bool
	StartTime     time.Time
	MFAVerified   bool
	RiskFactors   []string
}

// AuditEvent represents an audit log event
type AuditEvent struct {
	RequestID   string
	Timestamp   time.Time
	Method      string
	URL         string
	ClientIP    string
	UserAgent   string
	Headers     http.Header
	Metadata    metadata.MD
	Duration    time.Duration
	StatusCode  int
	BytesIn     int64
	BytesOut    int64
}

// SecurityEvent represents a security-related event
type SecurityEvent struct {
	Type      string
	RequestID string
	UserID    string
	ClientIP  string
	Resource  string
	Action    string
	Reason    string
	Error     string
	Timestamp time.Time
	Severity  string
}

// ThreatAnalysis represents threat analysis results
type ThreatAnalysis struct {
	RiskScore    float64
	Threats      []string
	Indicators   map[string]interface{}
	Recommended  []string
}

// PolicyRequest represents an authorization request
type PolicyRequest struct {
	Subject  string
	Action   string
	Resource string
	Context  *SecurityContext
}

// PolicyDecision represents an authorization decision
type PolicyDecision struct {
	Decision string // allow, deny
	Reason   string
	Policies []string
}
