# Migration Guide

This guide helps you migrate from traditional nginx+createrepo setups to Plus Artifacts Server.

## Overview

Plus Artifacts Server is designed as a drop-in replacement for nginx+createrepo solutions, offering:

- **Single binary deployment** instead of multiple services
- **Built-in API** instead of custom scripts
- **Real-time metadata generation** instead of cron jobs
- **Better performance** with lower resource usage

## Pre-Migration Assessment

### Current Setup Analysis

Before migrating, document your current setup:

1. **Repository Structure**:
   ```bash
   # Document current repositories
   find /var/www/repos -type d -name "repodata" | sed 's|/repodata||' | sed 's|/var/www/repos/||'
   ```

2. **Package Count**:
   ```bash
   # Count packages per repository
   for repo in $(ls /var/www/repos); do
     echo "$repo: $(find /var/www/repos/$repo -name "*.rpm" | wc -l) packages"
   done
   ```

3. **Storage Usage**:
   ```bash
   # Check disk usage
   du -sh /var/www/repos/*
   ```

4. **Current Configuration**:
   - nginx configuration files
   - createrepo cron jobs
   - Custom upload scripts
   - SSL certificates
   - Access logs and monitoring

### Compatibility Check

Plus is compatible with:
- ✅ Standard YUM repositories
- ✅ RPM packages (all architectures)
- ✅ Multi-level repository paths
- ✅ Standard repodata structure
- ✅ Existing YUM client configurations

Plus currently does NOT support:
- ❌ GPG signing (planned for v1.1)
- ❌ Delta RPMs (planned for v1.2)
- ❌ Custom metadata (planned for v1.2)

## Migration Strategies

### Strategy 1: Blue-Green Migration (Recommended)

Deploy Plus alongside existing setup, then switch traffic.

**Advantages:**
- Zero downtime
- Easy rollback
- Full testing before switch

**Steps:**

1. **Deploy Plus on different port**:
   ```bash
   # Install Plus
   wget https://github.com/elastic-io/plus/releases/latest/download/plus-linux-amd64
   chmod +x plus-linux-amd64
   mv plus-linux-amd64 /usr/local/bin/plus
   
   # Create config
   cat > /etc/plus/config.yaml << EOF
   server:
     listen: ":8081"
   storage:
     type: "local"
     path: "/var/lib/plus/storage"
   logging:
     level: "info"
   EOF
   
   # Start Plus
   plus --config /etc/plus/config.yaml
   ```

2. **Migrate data**:
   ```bash
   # Use migration script (see below)
   ./migrate.sh
   ```

3. **Test Plus setup**:
   ```bash
   # Test API
   curl http://localhost:8081/health
   curl http://localhost:8081/repos
   
   # Test YUM access
   yum --disablerepo="*" --enablerepo="plus-test" list available
   ```

4. **Switch traffic**:
   ```nginx
   # Update nginx config
   upstream repo_backend {
       server localhost:8081;  # Plus
       server localhost:80 backup;  # Old setup as backup
   }
   ```

5. **Monitor and verify**:
   ```bash
   # Monitor Plus metrics
   curl http://localhost:8081/metrics
   
   # Check logs
   tail -f /var/log/plus/plus.log
   ```

6. **Complete migration**:
   ```bash
   # Stop old services
   systemctl stop nginx
   systemctl stop createrepo-timer
   
   # Update Plus to use port 80
   # Remove backup configuration
   ```

### Strategy 2: Direct Migration

Replace existing setup directly.

**Advantages:**
- Simpler process
- Immediate benefits

**Disadvantages:**
- Potential downtime
- Harder rollback

**Steps:**

1. **Backup current setup**:
   ```bash
   # Backup repositories
   tar -czf repos-backup-$(date +%Y%m%d).tar.gz /var/www/repos
   
   # Backup nginx config
   cp -r /etc/nginx /etc/nginx.backup
   ```

2. **Stop existing services**:
   ```bash
   systemctl stop nginx
   systemctl stop createrepo-timer
   ```

3. **Install and configure Plus**:
   ```bash
   # Install Plus
   wget https://github.com/elastic-io/plus/releases/latest/download/plus-linux-amd64
   chmod +x plus-linux-amd64
   mv plus-linux-amd64 /usr/local/bin/plus
   
   # Migrate data
   ./migrate.sh
   
   # Start Plus
   plus --config /etc/plus/config.yaml
   ```

4. **Update client configurations**:
   ```bash
   # Update YUM repository files
   ./update-yum-configs.sh
   ```

### Strategy 3: Gradual Migration

Migrate repositories one by one.

**Steps:**

1. **Start with test repository**:
   ```bash
   # Create test repo in Plus
   curl -X POST http://localhost:8081/repos \
     -H "Content-Type: application/json" \
     -d '{"name": "test-repo"}'
   
   # Upload test packages
   curl -X POST http://localhost:8081/repo/test-repo/upload \
     -F "file=@test-package.rpm"
   ```

2. **Migrate production repositories gradually**:
   ```bash
   # Migrate one repository at a time
   for repo in repo1 repo2 repo3; do
     echo "Migrating $repo..."
     migrate_single_repo.sh $repo
     test_repo.sh $repo
   done
   ```

## Migration Scripts

### Complete Migration Script

```bash
#!/bin/bash
# migrate.sh - Complete migration script

set -e

# Configuration
OLD_REPO_PATH="/var/www/repos"
NEW_REPO_PATH="/var/lib/plus/storage"
PLUS_URL="http://localhost:8081"
BACKUP_DIR="/var/backups/plus-migration"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')] $1${NC}"
}

warn() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] WARNING: $1${NC}"
}

error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ERROR: $1${NC}"
    exit 1
}

# Pre-flight checks
preflight_checks() {
    log "Running pre-flight checks..."
    
    # Check if Plus is running
    if ! curl -s "$PLUS_URL/health" > /dev/null; then
        error "Plus server is not running at $PLUS_URL"
    fi
    
    # Check disk space
    REQUIRED_SPACE=$(du -s "$OLD_REPO_PATH" | cut -f1)
    AVAILABLE_SPACE=$(df "$(dirname "$NEW_REPO_PATH")" | tail -1 | awk '{print $4}')
    
    if [ "$REQUIRED_SPACE" -gt "$AVAILABLE_SPACE" ]; then
        error "Insufficient disk space. Required: ${REQUIRED_SPACE}KB, Available: ${AVAILABLE_SPACE}KB"
    fi
    
    # Check permissions
    if [ ! -w "$(dirname "$NEW_REPO_PATH")" ]; then
        error "No write permission to $NEW_REPO_PATH"
    fi
    
    log "Pre-flight checks passed"
}

# Create backup
create_backup() {
    log "Creating backup..."
    
    mkdir -p "$BACKUP_DIR"
    
    # Backup repositories
    tar -czf "$BACKUP_DIR/repos-$(date +%Y%m%d-%H%M%S).tar.gz" "$OLD_REPO_PATH"
    
    # Backup nginx config
    if [ -d "/etc/nginx" ]; then
        cp -r /etc/nginx "$BACKUP_DIR/nginx-$(date +%Y%m%d-%H%M%S)"
    fi
    
    log "Backup created in $BACKUP_DIR"
}

# Migrate single repository
migrate_repository() {
    local repo_name="$1"
    local repo_path="$OLD_REPO_PATH/$repo_name"
    
    log "Migrating repository: $repo_name"
    
    # Create repository in Plus
    curl -s -X POST "$PLUS_URL/repos" \
         -H "Content-Type: application/json" \
         -d "{\"name\":\"$repo_name\"}" > /dev/null
    
    if [ $? -ne 0 ]; then
        warn "Failed to create repository $repo_name via API, creating manually"
        mkdir -p "$NEW_REPO_PATH/$repo_name/Packages"
    fi
    
    # Copy RPM files
    if [ -d "$repo_path" ]; then
        log "  Copying packages..."
        find "$repo_path" -name "*.rpm" -exec cp {} "$NEW_REPO_PATH/$repo_name/Packages/" \;
        
        # Count packages
        local package_count=$(find "$NEW_REPO_PATH/$repo_name/Packages" -name "*.rpm" | wc -l)
        log "  Copied $package_count packages"
        
        # Refresh metadata
        log "  Refreshing metadata..."
        curl -s -X POST "$PLUS_URL/repo/$repo_name/refresh" > /dev/null
        
        if [ $? -eq 0 ]; then
            log "  Repository $repo_name migrated successfully"
        else
            warn "  Failed to refresh metadata for $repo_name"
        fi
    else
        warn "  Repository path $repo_path does not exist"
    fi
}

# Verify migration
verify_migration() {
    local repo_name="$1"
    
    log "Verifying repository: $repo_name"
    
    # Check API response
    local api_response=$(curl -s "$PLUS_URL/repo/$repo_name")
    if echo "$api_response" | grep -q "success"; then
        log "  API check passed"
    else
        warn "  API check failed"
        return 1
    fi
    
    # Check repodata
    if [ -f "$NEW_REPO_PATH/$repo_name/repodata/repomd.xml" ]; then
        log "  Metadata check passed"
    else
        warn "  Metadata check failed"
        return 1
    fi
    
    # Check package count
    local old_count=$(find "$OLD_REPO_PATH/$repo_name" -name "*.rpm" 2>/dev/null | wc -l)
    local new_count=$(find "$NEW_REPO_PATH/$repo_name/Packages" -name "*.rpm" 2>/dev/null | wc -l)
    
    if [ "$old_count" -eq "$new_count" ]; then
        log "  Package count check passed ($new_count packages)"
    else
        warn "  Package count mismatch: old=$old_count, new=$new_count"
        return 1
    fi
    
    return 0
}

# Update YUM configurations
update_yum_configs() {
    log "Updating YUM configurations..."
    
    # Find existing repo files
    local repo_files=$(find /etc/yum.repos.d -name "*.repo" -exec grep -l "baseurl.*localhost\|baseurl.*$(hostname)" {} \;)
    
    if [ -n "$repo_files" ]; then
        # Backup existing configs
        for file in $repo_files; do
            cp "$file" "$file.backup-$(date +%Y%m%d)"
        done
        
        # Update baseurl in repo files
        for file in $repo_files; do
            sed -i 's|baseurl=http://[^/]*/repos/\([^/]*\)|baseurl=http://localhost:8080/repo/\1/files/|g' "$file"
            log "  Updated $file"
        done
    else
        log "  No existing YUM configurations found to update"
    fi
}

# Generate new YUM configurations
generate_yum_configs() {
    log "Generating new YUM configurations..."
    
    # Get list of repositories from Plus
    local repos=$(curl -s "$PLUS_URL/repos" | grep -o '"[^"]*"' | grep -v status | grep -v success | tr -d '"')
    
    for repo in $repos; do
        local config_file="/etc/yum.repos.d/plus-${repo//\//-}.repo"
        
        cat > "$config_file" << EOF
[plus-$repo]
name=Plus Repository - $repo
baseurl=http://localhost:8080/repo/$repo/files/
enabled=1
gpgcheck=0
metadata_expire=300
priority=1
EOF
        log "  Created $config_file"
    done
}

# Main migration process
main() {
    log "Starting Plus migration process..."
    
    # Run pre-flight checks
    preflight_checks
    
    # Create backup
    create_backup
    
    # Get list of repositories
    if [ ! -d "$OLD_REPO_PATH" ]; then
        error "Old repository path $OLD_REPO_PATH does not exist"
    fi
    
    local repositories=$(ls "$OLD_REPO_PATH" 2>/dev/null || echo "")
    
    if [ -z "$repositories" ]; then
        warn "No repositories found in $OLD_REPO_PATH"
        exit 0
    fi
    
    log "Found repositories: $repositories"
    
    # Migrate each repository
    local failed_repos=""
    for repo in $repositories; do
        if [ -d "$OLD_REPO_PATH/$repo" ]; then
            migrate_repository "$repo"
            
            # Verify migration
            if ! verify_migration "$repo"; then
                failed_repos="$failed_repos $repo"
            fi
        fi
    done
    
    # Update YUM configurations
    update_yum_configs
    generate_yum_configs
    
    # Final report
    log "Migration completed!"
    
    if [ -n "$failed_repos" ]; then
        warn "Some repositories failed verification:$failed_repos"
        warn "Please check these repositories manually"
    fi
    
    log "Next steps:"
    log "1. Test YUM operations: yum clean all && yum makecache"
    log "2. Verify package installations work correctly"
    log "3. Monitor Plus metrics: curl $PLUS_URL/metrics"
    log "4. Check Plus logs for any issues"
    log "5. Once satisfied, stop old services and update firewall rules"
}

# Run main function
main "$@"
```

### Single Repository Migration Script

```bash
#!/bin/bash
# migrate_single_repo.sh - Migrate a single repository

REPO_NAME="$1"
OLD_REPO_PATH="/var/www/repos"
NEW_REPO_PATH="/var/lib/plus/storage"
PLUS_URL="http://localhost:8081"

if [ -z "$REPO_NAME" ]; then
    echo "Usage: $0 <repository-name>"
    exit 1
fi

echo "Migrating repository: $REPO_NAME"

# Create repository
curl -X POST "$PLUS_URL/repos" \
     -H "Content-Type: application/json" \
     -d "{\"name\":\"$REPO_NAME\"}"

# Copy packages
mkdir -p "$NEW_REPO_PATH/$REPO_NAME/Packages"
find "$OLD_REPO_PATH/$REPO_NAME" -name "*.rpm" -exec cp {} "$NEW_REPO_PATH/$REPO_NAME/Packages/" \;

# Refresh metadata
curl -X POST "$PLUS_URL/repo/$REPO_NAME/refresh"

echo "Repository $REPO_NAME migrated successfully"
```

### YUM Configuration Update Script

```bash
#!/bin/bash
# update_yum_configs.sh - Update YUM repository configurations

PLUS_URL="http://localhost:8080"

# Get repositories from Plus
REPOS=$(curl -s "$PLUS_URL/repos" | jq -r '.repositories[]' 2>/dev/null || echo "")

if [ -z "$REPOS" ]; then
    echo "No repositories found or jq not installed"
    exit 1
fi

echo "Updating YUM configurations for Plus repositories..."

for repo in $REPOS; do
    # Create safe filename
    SAFE_NAME=$(echo "$repo" | tr '/' '-')
    CONFIG_FILE="/etc/yum.repos.d/plus-${SAFE_NAME}.repo"
    
    cat > "$CONFIG_FILE" << EOF
[plus-${SAFE_NAME}]
name=Plus Repository - $repo
baseurl=$PLUS_URL/repo/$repo/files/
enabled=1
gpgcheck=0
metadata_expire=300
priority=1
EOF
    
    echo "Created $CONFIG_FILE"
done

echo "YUM configurations updated. Run 'yum clean all && yum makecache' to refresh."
```

## Post-Migration Tasks

### 1. Verification Checklist

After migration, verify everything works:

- [ ] **API Health Check**:
  ```bash
  curl http://localhost:8080/health
  curl http://localhost:8080/ready
  ```

- [ ] **Repository List**:
  ```bash
  curl http://localhost:8080/repos
  ```

- [ ] **Package Count Verification**:
  ```bash
  # Compare package counts
  for repo in $(curl -s http://localhost:8080/repos | jq -r '.repositories[]'); do
    echo "Checking $repo..."
    curl -s "http://localhost:8080/repo/$repo" | jq '.package_count'
  done
  ```

- [ ] **YUM Operations**:
  ```bash
  # Clear YUM cache
  yum clean all
  
  # Rebuild cache
  yum makecache
  
  # List available packages
  yum list available
  
  # Test package installation
  yum install <test-package>
  ```

- [ ] **Metadata Verification**:
  ```bash
  # Check repodata files
  curl -I http://localhost:8080/repo/my-repo/files/repodata/repomd.xml
  
  # Verify primary metadata
  curl http://localhost:8080/repo/my-repo/files/repodata/repomd.xml | grep primary
  ```

### 2. Performance Monitoring

Monitor Plus performance after migration:

```bash
# Check metrics
curl http://localhost:8080/metrics

# Monitor resource usage
top -p $(pgrep plus)
htop -p $(pgrep plus)

# Check disk usage
du -sh /var/lib/plus/storage/*

# Monitor logs
tail -f /var/log/plus/plus.log
```

### 3. Client Configuration Updates

Update all client systems:

#### YUM Clients

```bash
# On each client system
yum clean all
yum makecache

# Test repository access
yum repolist
yum search <package-name>
```

#### Automated Client Updates

```bash
#!/bin/bash
# update_clients.sh - Update multiple client systems

CLIENTS="client1 client2 client3"
PLUS_SERVER="your-plus-server.com"

for client in $CLIENTS; do
    echo "Updating $client..."
    
    ssh "$client" << EOF
        # Backup existing configs
        sudo cp -r /etc/yum.repos.d /etc/yum.repos.d.backup
        
        # Download new configs
        sudo curl -o /etc/yum.repos.d/plus-repos.repo \
             http://$PLUS_SERVER:8080/yum-config
        
        # Update cache
        sudo yum clean all
        sudo yum makecache
        
        echo "Client $client updated successfully"
EOF
done
```

### 4. Monitoring and Alerting

Set up monitoring for Plus:

#### Basic Monitoring Script

```bash
#!/bin/bash
# monitor_plus.sh - Basic Plus monitoring

PLUS_URL="http://localhost:8080"
ALERT_EMAIL="admin@example.com"

# Check health
if ! curl -s "$PLUS_URL/health" | grep -q "healthy"; then
    echo "Plus health check failed" | mail -s "Plus Alert" "$ALERT_EMAIL"
fi

# Check metrics
METRICS=$(curl -s "$PLUS_URL/metrics")
ERROR_COUNT=$(echo "$METRICS" | jq '.requests.errors')
ACTIVE_REQUESTS=$(echo "$METRICS" | jq '.requests.active')

if [ "$ERROR_COUNT" -gt 100 ]; then
    echo "High error count: $ERROR_COUNT" | mail -s "Plus Alert" "$ALERT_EMAIL"
fi

if [ "$ACTIVE_REQUESTS" -gt 50 ]; then
    echo "High active requests: $ACTIVE_REQUESTS" | mail -s "Plus Alert" "$ALERT_EMAIL"
fi
```

#### Systemd Service for Monitoring

```ini
# /etc/systemd/system/plus-monitor.service
[Unit]
Description=Plus Monitoring Service
After=plus.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/monitor_plus.sh
User=plus
```

```ini
# /etc/systemd/system/plus-monitor.timer
[Unit]
Description=Run Plus monitoring every 5 minutes
Requires=plus-monitor.service

[Timer]
OnCalendar=*:0/5
Persistent=true

[Install]
WantedBy=timers.target
```

## Rollback Procedures

### Emergency Rollback

If issues occur, quickly rollback to the original setup:

```bash
#!/bin/bash
# rollback.sh - Emergency rollback script

echo "Starting emergency rollback..."

# Stop Plus
systemctl stop plus

# Restore nginx configuration
cp -r /etc/nginx.backup/* /etc/nginx/

# Start nginx
systemctl start nginx

# Restore createrepo cron job
systemctl start createrepo-timer

# Restore YUM configurations
cp /etc/yum.repos.d/*.backup /etc/yum.repos.d/
rm /etc/yum.repos.d/plus-*.repo

# Clear YUM cache
yum clean all

echo "Rollback completed"
```

### Gradual Rollback

For gradual rollback of specific repositories:

```bash
#!/bin/bash
# rollback_repo.sh - Rollback specific repository

REPO_NAME="$1"

if [ -z "$REPO_NAME" ]; then
    echo "Usage: $0 <repository-name>"
    exit 1
fi

echo "Rolling back repository: $REPO_NAME"

# Update YUM config to point back to nginx
sed -i "s|baseurl=http://localhost:8080/repo/$REPO_NAME/files/|baseurl=http://localhost/repos/$REPO_NAME/|g" \
    "/etc/yum.repos.d/plus-${REPO_NAME//\//-}.repo"

# Refresh YUM cache
yum clean all
yum makecache

echo "Repository $REPO_NAME rolled back"
```

## Troubleshooting Common Issues

### Issue 1: Package Count Mismatch

**Symptoms**: Different number of packages in old vs new repository

**Solution**:
```bash
# Find missing packages
OLD_PACKAGES=$(find /var/www/repos/my-repo -name "*.rpm" | sort)
NEW_PACKAGES=$(find /var/lib/plus/storage/my-repo/Packages -name "*.rpm" | sort)

diff <(echo "$OLD_PACKAGES") <(echo "$NEW_PACKAGES")

# Copy missing packages
for pkg in $(comm -23 <(echo "$OLD_PACKAGES") <(echo "$NEW_PACKAGES")); do
    cp "$pkg" /var/lib/plus/storage/my-repo/Packages/
done

# Refresh metadata
curl -X POST http://localhost:8080/repo/my-repo/refresh
```

### Issue 2: Metadata Generation Fails

**Symptoms**: Empty or missing repodata directory

**Solution**:
```bash
# Check Plus logs
tail -f /var/log/plus/plus.log

# Manually refresh metadata
curl -X POST http://localhost:8080/repo/my-repo/refresh

# Check file permissions
chown -R plus:plus /var/lib/plus/storage
chmod -R 755 /var/lib/plus/storage
```

### Issue 3: YUM Cache Issues

**Symptoms**: YUM can't find packages after migration

**Solution**:
```bash
# Clear all YUM caches
yum clean all
rm -rf /var/cache/yum/*

# Rebuild cache
yum makecache

# Check repository configuration
yum repolist -v

# Test specific repository
yum --disablerepo="*" --enablerepo="plus-my-repo" list available
```

### Issue 4: Performance Degradation

**Symptoms**: Slower package operations after migration

**Solution**:
```bash
# Check Plus metrics
curl http://localhost:8080/metrics

# Monitor resource usage
htop -p $(pgrep plus)

# Check disk I/O
iotop -p $(pgrep plus)

# Optimize configuration
# Increase worker processes, adjust timeouts
```

## Best Practices

### 1. Migration Planning

- **Test in staging environment first**
- **Plan for maintenance window**
- **Communicate with users**
- **Prepare rollback procedures**
- **Document custom configurations**

### 2. Data Integrity

- **Verify checksums** of migrated packages
- **Compare package counts** before and after
- **Test critical packages** after migration
- **Monitor for missing dependencies**

### 3. Performance Optimization

- **Use SSD storage** for better I/O performance
- **Allocate sufficient memory** for metadata caching
- **Monitor network bandwidth** usage
- **Optimize client configurations**

### 4. Security Considerations

- **Update firewall rules** for new ports
- **Configure SSL/TLS** if required
- **Set up proper file permissions**
- **Monitor access logs**

## Migration Timeline

### Typical Migration Schedule

**Week 1: Planning and Preparation**
- [ ] Assess current setup
- [ ] Plan migration strategy
- [ ] Set up test environment
- [ ] Prepare migration scripts

**Week 2: Testing**
- [ ] Test migration scripts
- [ ] Verify functionality
- [ ] Performance testing
- [ ] User acceptance testing

**Week 3: Production Migration**
- [ ] Schedule maintenance window
- [ ] Execute migration
- [ ] Verify functionality
- [ ] Monitor performance

**Week 4: Optimization and Cleanup**
- [ ] Optimize configuration
- [ ] Clean up old files
- [ ] Update documentation
- [ ] Train users

## Support and Resources

### Getting Help

- **GitHub Issues**: Report migration problems
- **Documentation**: Comprehensive guides and examples
- **Community**: Discussion forums and chat
- **Professional Support**: Available for enterprise users

### Useful Resources

- [Plus API Documentation](api.md)
- [Configuration Reference](configuration.md)
- [Performance Tuning Guide](performance.md)
- [Security Best Practices](security.md)

## Conclusion

Migrating from nginx+createrepo to Plus Artifacts Server provides significant benefits in terms of performance, maintainability, and features. With proper planning and execution, the migration can be completed with minimal disruption to users.

The key to successful migration is:
1. **Thorough testing** in a staging environment
2. **Gradual rollout** when possible
3. **Comprehensive monitoring** during and after migration
4. **Quick rollback procedures** if issues arise

For additional support or questions about migration, please refer to the [GitHub repository](https://github.com/elastic-io/plus) or contact the development team.