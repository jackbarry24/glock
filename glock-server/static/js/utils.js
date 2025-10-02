/**
 * Utility functions for the Glock Dashboard
 */

// API base URL
const API_BASE = '/api';

// Format duration from seconds to human readable
function formatDuration(seconds) {
    if (seconds <= 0) return 'Expired';

    const units = [
        { name: 'd', seconds: 86400 },
        { name: 'h', seconds: 3600 },
        { name: 'm', seconds: 60 },
        { name: 's', seconds: 1 }
    ];

    let result = '';
    let remaining = Math.floor(seconds);

    for (const unit of units) {
        const count = Math.floor(remaining / unit.seconds);
        if (count > 0 || (result === '' && unit.name === 's')) {
            result += count + unit.name + ' ';
            remaining %= unit.seconds;
        }
    }

    return result.trim() || '0s';
}

// Format time remaining with color coding
function formatTimeRemaining(lock) {
    // If lock is frozen or available, show "-" instead of time
    if (lock.frozen || !lock.owner_id) {
        return '<span class="time-normal">-</span>';
    }

    const now = new Date();
    const acquiredAt = new Date(lock.acquired_at);
    const lastRefresh = new Date(lock.last_refresh);
    const maxTTLExpiry = new Date(acquiredAt.getTime() + lock.max_ttl / 1000000);
    const ttlExpiry = new Date(lastRefresh.getTime() + lock.ttl / 1000000);

    // Use the earliest expiry time
    const expiryTime = ttlExpiry < maxTTLExpiry ? ttlExpiry : maxTTLExpiry;
    const secondsRemaining = Math.floor((expiryTime - now) / 1000);

    if (secondsRemaining <= 0) {
        return '<span class="time-expired">Expired</span>';
    }

    const formatted = formatDuration(secondsRemaining);
    const cssClass = secondsRemaining < 60 ? 'time-warning' : 'time-normal';

    return `<span class="${cssClass}">${formatted}</span>`;
}

// Format lock status with badge
function formatStatus(lock, ancestorInfo = { isBlocked: false }) {
    let statusText = '';
    let statusClass = '';
    let tooltip = '';

    if (lock.frozen) {
        statusText = 'Frozen';
        statusClass = 'status-frozen';
    } else if (ancestorInfo.isBlocked) {
        statusText = 'Blocked';
        statusClass = 'status-blocked';
        tooltip = `Parent lock "${escapeHtml(ancestorInfo.blockedBy)}" is held by ${escapeHtml(ancestorInfo.blockedByOwner)}`;
    } else if (!lock.owner_id) {
        statusText = 'Available';
        statusClass = 'status-available';
    } else {
        const now = new Date();
        const acquiredAt = new Date(lock.acquired_at);
        const lastRefresh = new Date(lock.last_refresh);
        const maxTTLExpiry = new Date(acquiredAt.getTime() + lock.max_ttl / 1000000);
        const ttlExpiry = new Date(lastRefresh.getTime() + lock.ttl / 1000000);

        if (now > maxTTLExpiry || now > ttlExpiry) {
            statusText = 'Expired';
            statusClass = 'status-expired';
        } else {
            statusText = 'Held';
            statusClass = 'status-held';
        }
    }

    const titleAttr = tooltip ? ` title="${tooltip}"` : '';
    return `<span class="status-badge ${statusClass}"${titleAttr}>${statusText}</span>`;
}

// Format owner display
function formatOwner(lock, ancestorInfo = { isBlocked: false }) {
    // If blocked by parent, show parent owner info
    if (ancestorInfo.isBlocked) {
        const blockedOwnerShort = ancestorInfo.blockedByOwner ? ancestorInfo.blockedByOwner.split('-')[0] : '';
        return `<span class="owner-blocked" title="Blocked by parent: ${escapeHtml(ancestorInfo.blockedBy)} (${escapeHtml(ancestorInfo.blockedByOwner)})">— (${escapeHtml(blockedOwnerShort)})</span>`;
    }

    // If lock is frozen or available, show "-" instead of owner
    if (lock.frozen || !lock.owner_id) {
        return '<span class="owner-none">-</span>';
    }

    // Get full owner info for tooltip
    const fullOwnerId = lock.owner_id || '';
    const fullOwner = lock.owner || lock.owner_id;

    // Display only first segment of UUID
    const displayName = fullOwnerId ? fullOwnerId.split('-')[0] : (lock.owner || lock.owner_id);

    return `<strong title="${escapeHtml(fullOwner)}">${escapeHtml(displayName)}</strong>`;
}

// Format queue information
function formatQueueInfo(lock) {
    // Check for no queue
    if (!lock || !lock.queue_type || lock.queue_type === 'none') {
        return '-';
    }

    // Get queue size
    const queueSize = lock.queue_size || 0;

    // Return plain text format: "Empty (TYPE)" or button for non-empty queues
    if (queueSize === 0) {
        return `Empty (${lock.queue_type.toUpperCase()})`;
    }

    // Return button for queues with items
    return `<button class="btn btn-info btn-xs queue-details-btn" data-lock-name="${lock.name}" title="View queue details">${queueSize} in Queue</button>`;
}

// Format TTL durations
function formatTTL(nanoseconds) {
    // Convert nanoseconds to seconds
    const seconds = nanoseconds / 1000000000;
    return formatDuration(seconds);
}

// Generate action buttons for a lock
function generateActionButtons(lock, ancestorInfo = { isBlocked: false }) {
    const actions = [];

    // Metrics button (always available)
    const metricsBtn = `<button class="metrics-btn" data-lock-name="${lock.name}" title="View metrics">Metrics</button>`;
    actions.push(metricsBtn);

    // Acquire button (only if not currently held and not blocked)
    if (!lock.owner_id && !ancestorInfo.isBlocked && !lock.frozen) {
        actions.push(`<button class="btn btn-success btn-sm action-btn acquire-btn" data-lock-name="${lock.name}">Acquire</button>`);
    }

    // Update button (always available for existing locks)
    actions.push(`<button class="btn btn-primary btn-sm action-btn update-btn" data-lock-name="${lock.name}">Update</button>`);

    // Release button (only if currently held)
    if (lock.owner_id) {
        actions.push(`<button class="btn btn-warning btn-sm action-btn release-btn" data-lock-name="${lock.name}">Release</button>`);
    }

    // Freeze/Unfreeze buttons
    if (lock.frozen) {
        actions.push(`<button class="btn btn-info btn-sm action-btn unfreeze-btn" data-lock-name="${lock.name}">Unfreeze</button>`);
    } else {
        actions.push(`<button class="btn btn-secondary btn-sm action-btn freeze-btn" data-lock-name="${lock.name}">Freeze</button>`);
    }

    // Delete button (only if not currently held and not blocked)
    if (!lock.owner_id && !ancestorInfo.isBlocked) {
        actions.push(`<button class="btn btn-danger btn-sm action-btn delete-btn" data-lock-name="${lock.name}">Delete</button>`);
    }

    return actions.join(' ');
}

// Generate a random UUID v4
function generateUUID() {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function (c) {
        const r = Math.random() * 16 | 0;
        const v = c === 'x' ? r : (r & 0x3 | 0x8);
        return v.toString(16);
    });
}

// Escape HTML to prevent XSS
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Show loading indicator
function showLoading() {
    const indicator = document.getElementById('loading-indicator');
    if (indicator) {
        indicator.style.display = 'block';
        indicator.textContent = 'Loading...';
    }
}

// Hide loading indicator
function hideLoading() {
    const indicator = document.getElementById('loading-indicator');
    if (indicator) {
        indicator.style.display = 'none';
    }
}

// Show alert modal
function showAlert(title, message, type = 'info') {
    const modal = document.getElementById('alert-modal');
    const titleEl = document.getElementById('alert-title');
    const messageEl = document.getElementById('alert-message');

    if (modal && titleEl && messageEl) {
        titleEl.textContent = title;
        messageEl.textContent = message;

        // Update modal styling based on type
        modal.className = `modal alert-${type}`;

        modal.style.display = 'flex';
    }
}

// Hide alert modal
function hideAlert() {
    const modal = document.getElementById('alert-modal');
    if (modal) {
        modal.style.display = 'none';
    }
}

// Show confirmation modal
function showConfirm(title, message, onConfirm) {
    const modal = document.getElementById('confirm-modal');
    const titleEl = document.getElementById('confirm-title');
    const messageEl = document.getElementById('confirm-message');
    const confirmBtn = document.getElementById('confirm-action');

    if (modal && titleEl && messageEl && confirmBtn) {
        titleEl.textContent = title;
        messageEl.textContent = message;

        // Remove previous event listeners
        const newConfirmBtn = confirmBtn.cloneNode(true);
        confirmBtn.parentNode.replaceChild(newConfirmBtn, confirmBtn);

        // Add new event listener
        newConfirmBtn.addEventListener('click', () => {
            hideConfirm();
            if (onConfirm) onConfirm();
        });

        modal.style.display = 'flex';
    }
}

// Hide confirmation modal
function hideConfirm() {
    const modal = document.getElementById('confirm-modal');
    if (modal) {
        modal.style.display = 'none';
    }
}

// Format last updated time
function formatLastUpdated(date) {
    if (!date) return 'Never';

    const now = new Date();
    const diffMs = now - date;
    const diffSeconds = Math.floor(diffMs / 1000);
    const diffMinutes = Math.floor(diffSeconds / 60);
    const diffHours = Math.floor(diffMinutes / 60);

    if (diffSeconds < 60) {
        return 'Just now';
    } else if (diffMinutes < 60) {
        return `${diffMinutes}m ago`;
    } else if (diffHours < 24) {
        return `${diffHours}h ago`;
    } else {
        return date.toLocaleDateString();
    }
}

// Validate duration string (basic validation)
function isValidDuration(duration) {
    // Basic regex for Go duration format
    const durationRegex = /^(\d+[smhd])+$/;
    return durationRegex.test(duration.replace(/\s/g, ''));
}

// Parse JSON safely
function safeJsonParse(jsonString) {
    try {
        return JSON.parse(jsonString);
    } catch (e) {
        return null;
    }
}

// Format metadata for display
function formatMetadata(metadata) {
    if (!metadata) return '';

    try {
        const parsed = typeof metadata === 'string' ? JSON.parse(metadata) : metadata;
        return JSON.stringify(parsed, null, 2);
    } catch (e) {
        return String(metadata);
    }
}

// Debounce function for search input
function debounce(func, wait) {
    let timeout;
    return function executedFunction(...args) {
        const later = () => {
            clearTimeout(timeout);
            func(...args);
        };
        clearTimeout(timeout);
        timeout = setTimeout(later, wait);
    };
}

// Export functions to global scope
window.Utils = {
    formatDuration,
    formatTimeRemaining,
    formatStatus,
    formatOwner,
    formatQueueInfo,
    formatTTL,
    generateActionButtons,
    generateUUID,
    escapeHtml,
    showLoading,
    hideLoading,
    showAlert,
    hideAlert,
    showConfirm,
    hideConfirm,
    formatLastUpdated,
    isValidDuration,
    safeJsonParse,
    formatMetadata,
    debounce
};
