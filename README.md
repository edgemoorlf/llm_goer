# Azure OpenAI Proxy

A high-performance, production-ready proxy service for Azure OpenAI API built in Go. This service provides intelligent load balancing, rate limiting, and comprehensive monitoring for Azure OpenAI deployments while maintaining full compatibility with the OpenAI API format.

## 🚀 Features

- **OpenAI API Compatibility**: Drop-in replacement for OpenAI API clients
- **Multi-Instance Load Balancing**: Intelligent routing across multiple Azure OpenAI instances
- **Advanced Rate Limiting**: Redis-based sliding window rate limiting with token-aware management
- **Real-time Streaming**: Full support for Server-Sent Events (SSE) streaming responses
- **Health Monitoring**: Automatic health checks and failover for unhealthy instances
- **Comprehensive Statistics**: Real-time metrics and usage analytics
- **Production Ready**: Docker containerization with security best practices
- **High Performance**: Optimized for low latency and high throughput

## 📋 Supported Endpoints

- `POST /v1/chat/completions` - Chat completions with streaming support
- `POST /v1/completions` - Text completions
- `POST /v1/embeddings` - Text embeddings
- `GET /admin/instances` - Instance management and monitoring
- `GET /stats/` - Usage statistics and analytics

## 🛠 Quick Start

### Using Docker Compose (Recommended)

1. **Clone the repository**
```bash
git clone <repository-url>
cd azure-openai-proxy
```

2. **Configure environment variables**
```bash
cp .env.example .env
# Edit .env with your Azure OpenAI credentials
```

3. **Start the services**
```bash
docker-compose up -d
```

4. **Verify the deployment**
```bash
curl http://localhost:8080/health
```

### Manual Installation

1. **Prerequisites**
   - Go 1.21 or later
   - Redis server
   - SQLite

2. **Build and run**
```bash
go mod download
go build -o bin/proxy cmd/proxy/main.go
./bin/proxy -config configs/
```

## ⚙️ Configuration

### Environment Variables

Configure your Azure OpenAI instances using environment variables:

```bash
# Primary Azure OpenAI instance
AZURE_API_KEY_PRIMARY=your-azure-api-key
AZURE_API_BASE_PRIMARY=https://your-resource.openai.azure.com

# Secondary instance (optional)
AZURE_API_KEY_SECONDARY=your-secondary-api-key
AZURE_API_BASE_SECONDARY=https://your-secondary-resource.openai.azure.com

# Redis configuration
REDIS_URL=redis://localhost:6379

# Application settings
PORT=8080
LOG_LEVEL=INFO
ENVIRONMENT=production
```

### Configuration Files

The service uses YAML configuration files in the `configs/` directory:

- `base.yaml` - Base configuration
- `production.yaml` - Production-specific overrides

Example configuration:

```yaml
name: "Azure OpenAI Proxy"
port: 8080

routing:
  strategy: "weighted"  # failover, weighted, round_robin
  retries: 3
  timeout: 30

instances:
  - name: "azure-primary"
    provider_type: "azure"
    api_key: "${AZURE_API_KEY_PRIMARY}"
    api_base: "${AZURE_API_BASE_PRIMARY}"
    api_version: "2024-05-01-preview"
    priority: 1
    weight: 10
    max_tpm: 60000
    supported_models:
      - "gpt-4"
      - "gpt-4o"
      - "gpt-35-turbo"
    model_deployments:
      "gpt-4": "gpt-4-deployment"
      "gpt-4o": "gpt-4o-deployment"
    enabled: true
```

## 🔧 Usage Examples

### Chat Completions

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt-4",
    "messages": [
      {"role": "user", "content": "Hello, how are you?"}
    ],
    "stream": false
  }'
```

### Streaming Chat Completions

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt-4",
    "messages": [
      {"role": "user", "content": "Tell me a story"}
    ],
    "stream": true
  }'
```

### Using with OpenAI SDK

```python
import openai

client = openai.OpenAI(
    api_key="your-api-key",
    base_url="http://localhost:8080/v1"
)

response = client.chat.completions.create(
    model="gpt-4",
    messages=[{"role": "user", "content": "Hello!"}]
)
```

## 📊 Monitoring and Administration

### Health Check

```bash
curl http://localhost:8080/health
```

### Instance Status

```bash
curl http://localhost:8080/admin/instances
```

### Usage Statistics

```bash
# Overall statistics
curl http://localhost:8080/stats/

# Instance-specific statistics
curl http://localhost:8080/stats/instances?instance=azure-primary

# Usage metrics with time series
curl http://localhost:8080/stats/usage?metric=tokens&window=60
```

### Admin Operations

```bash
# Reset instance state
curl -X POST http://localhost:8080/admin/instances/azure-primary/reset

# Update instance configuration
curl -X PUT http://localhost:8080/admin/instances/azure-primary/config \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

## 🏗 Architecture

### Core Components

- **Instance Manager**: Manages multiple Azure OpenAI instances with health monitoring
- **Rate Limiter**: Redis-based sliding window rate limiting with token awareness
- **Request Transformer**: Converts OpenAI API requests to Azure OpenAI format
- **Load Balancer**: Intelligent request routing with multiple strategies
- **Statistics Engine**: Real-time metrics collection and analysis

### Selection Strategies

- **Failover**: Route to highest priority healthy instance
- **Weighted**: Distribute requests based on configured weights
- **Round Robin**: Rotate requests among healthy instances
- **Composite**: Advanced scoring based on latency, utilization, and health

### Rate Limiting

- Token-aware rate limiting based on Azure OpenAI TPM limits
- Sliding window implementation with Redis backend
- Per-instance rate limit tracking
- Automatic retry-after header generation

## 🔒 Security

- **Authentication**: Optional admin token authentication
- **CORS**: Configurable CORS headers
- **Security Headers**: Comprehensive security headers
- **Input Validation**: Request payload validation
- **Error Handling**: Secure error responses without sensitive data exposure

## 🐳 Docker Deployment

### Docker Compose Services

- **proxy**: Main application service
- **redis**: State storage and rate limiting
- **redis-commander**: Optional Redis management UI

### Health Checks

The service includes comprehensive health checks:
- Application health endpoint
- Redis connectivity
- Instance health monitoring
- Automatic container restart on failure

## 📈 Performance

### Optimizations

- **Concurrent Processing**: Goroutines for health checks and monitoring
- **Connection Pooling**: HTTP client connection reuse
- **Memory Efficiency**: Optimized data structures and garbage collection
- **Caching**: Instance state caching with Redis
- **Streaming**: Efficient SSE implementation for real-time responses

### Monitoring Metrics

- Request latency and throughput
- Token usage and rate limit utilization
- Instance health and error rates
- Memory and CPU usage
- Connection pool statistics

## 🛠 Development

### Building from Source

```bash
# Install dependencies
go mod download

# Run tests
go test ./...

# Build
go build -o bin/proxy cmd/proxy/main.go

# Run locally
./bin/proxy -config configs/
```

### Project Structure

```
├── cmd/proxy/          # Main application
├── internal/
│   ├── config/         # Configuration management
│   ├── handlers/       # HTTP handlers
│   ├── instance/       # Instance management
│   ├── middleware/     # HTTP middleware
│   ├── services/       # Business logic
│   ├── storage/        # Data storage
│   ├── utils/          # Utilities
│   └── errors/         # Error handling
├── configs/            # Configuration files
├── pkg/                # Public packages
└── docker-compose.yml  # Docker deployment
```

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🤝 Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 🆘 Support

- **Issues**: Report bugs and feature requests via GitHub Issues
- **Documentation**: Comprehensive documentation in `/docs`
- **Community**: Join our discussions for questions and support

---

**Built with ❤️ in Go for high-performance Azure OpenAI API proxying**