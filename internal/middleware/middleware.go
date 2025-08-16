package middleware

import (
	"time"
	
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// RequestLogger creates a custom request logging middleware
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		
		c.Next()
		
		// Log after request completion
		duration := time.Since(start)
		logrus.WithFields(logrus.Fields{
			"timestamp":   start.Format(time.RFC3339),
			"status_code": c.Writer.Status(),
			"latency":     duration,
			"client_ip":   c.ClientIP(),
			"method":      c.Request.Method,
			"path":        c.Request.URL.Path,
			"user_agent":  c.Request.UserAgent(),
			"request_id":  c.GetString("request_id"),
		}).Info("HTTP Request")
	}
}

// RequestID adds a unique request ID to each request
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.Request.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
		}
		
		c.Header("X-Request-ID", requestID)
		c.Set("request_id", requestID)
		
		c.Next()
	}
}

// CORS adds CORS headers
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, Authorization, X-Request-ID")
		c.Header("Access-Control-Expose-Headers", "X-Request-ID, Retry-After")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		
		c.Next()
	}
}

// RateLimit middleware for basic rate limiting (simplified implementation)
func RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Basic implementation - in production, this would use Redis
		// For now, just add headers and continue
		c.Header("X-RateLimit-Remaining", "1000")
		c.Header("X-RateLimit-Reset", "3600")
		c.Next()
	}
}

// Authentication middleware for admin endpoints
func AdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Basic implementation - check for admin token
		adminToken := c.GetHeader("X-Admin-Token")
		if adminToken == "" {
			c.JSON(401, gin.H{"error": "Admin token required"})
			c.Abort()
			return
		}
		
		// In production, validate against configured admin tokens
		// For now, accept any non-empty token
		c.Next()
	}
}

// Metrics middleware to collect request metrics
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		
		c.Next()
		
		// Record metrics after request completion
		duration := time.Since(start)
		
		logrus.WithFields(logrus.Fields{
			"path":        c.Request.URL.Path,
			"method":      c.Request.Method,
			"status_code": c.Writer.Status(),
			"duration_ms": duration.Milliseconds(),
			"request_size": c.Request.ContentLength,
			"response_size": c.Writer.Size(),
		}).Debug("Request metrics")
	}
}

// SecurityHeaders adds security headers
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", "default-src 'self'")
		c.Next()
	}
}

// generateRequestID creates a simple request ID
func generateRequestID() string {
	// Simple implementation - in production use UUID or similar
	return time.Now().Format("20060102150405") + "-" + "req"
}