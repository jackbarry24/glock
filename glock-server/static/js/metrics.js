/**
 * Metrics functionality for the Glock Dashboard
 */

class MetricsManager {
    constructor() {
        this.currentLockName = null;
        this.ownerHistoryPage = 1;
        this.ownerHistoryPageSize = 10;
        this.ownerHistoryData = [];
        this.init();
    }

    init() {
        this.bindEvents();
    }

    bindEvents() {
        // Refresh metrics button
        const refreshBtn = document.getElementById('refresh-metrics-btn');
        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => {
                if (this.currentLockName) {
                    this.loadMetrics(this.currentLockName);
                }
            });
        }

        // Pagination buttons
        const prevBtn = document.getElementById('owner-history-prev');
        const nextBtn = document.getElementById('owner-history-next');
        if (prevBtn) {
            prevBtn.addEventListener('click', () => this.changeOwnerHistoryPage(-1));
        }
        if (nextBtn) {
            nextBtn.addEventListener('click', () => this.changeOwnerHistoryPage(1));
        }

        // Modal close events
        document.addEventListener('click', (e) => {
            const metricsModal = document.getElementById('metrics-modal');
            if (metricsModal && (e.target.classList.contains('modal') || e.target.classList.contains('modal-close') || e.target.classList.contains('modal-cancel'))) {
                this.closeMetricsModal();
            }
        });
    }

    async showMetricsModal(lockName) {
        this.currentLockName = lockName;

        const modal = document.getElementById('metrics-modal');
        const titleEl = document.getElementById('metrics-title');

        if (modal && titleEl) {
            titleEl.textContent = `Metrics - ${lockName}`;
            modal.style.display = 'flex';

            await this.loadMetrics(lockName);
        }
    }

    closeMetricsModal() {
        const modal = document.getElementById('metrics-modal');
        if (modal) {
            modal.style.display = 'none';
        }
        this.currentLockName = null;
        this.ownerHistoryPage = 1;
        this.ownerHistoryData = [];
    }

    async loadMetrics(lockName) {
        const loadingEl = document.getElementById('metrics-loading');
        const contentEl = document.getElementById('metrics-content');

        try {
            // Show loading
            if (loadingEl) loadingEl.style.display = 'block';
            if (contentEl) contentEl.style.display = 'none';

            const response = await fetch(`/api/metrics/${encodeURIComponent(lockName)}`);

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            const data = await response.json();
            this.displayMetrics(data);

        } catch (error) {
            console.error('Error loading metrics:', error);
            this.showMetricsError(error.message);
        } finally {
            // Hide loading
            if (loadingEl) loadingEl.style.display = 'none';
            if (contentEl) contentEl.style.display = 'block';
        }
    }

    displayMetrics(data) {
        if (!data || !data.metrics) {
            this.showMetricsError('No metrics data available');
            return;
        }

        const metrics = data.metrics;

        // Update usage statistics
        this.setMetricValue('metric-total-attempts', metrics.total_acquire_attempts || 0);
        this.setMetricValue('metric-successful', metrics.successful_acquires || 0);
        this.setMetricValue('metric-failed', metrics.failed_acquires || 0);

        // Calculate and display success rate
        const totalAttempts = metrics.total_acquire_attempts || 0;
        const successful = metrics.successful_acquires || 0;
        const successRate = totalAttempts > 0 ? Math.round((successful / totalAttempts) * 100) : 0;
        this.setMetricValue('metric-success-rate', `${successRate}%`, 'success-rate');

        // Update queue statistics
        this.setMetricValue('metric-current-queue', metrics.current_queue_size || 0);
        this.setMetricValue('metric-total-queued', metrics.total_queued_requests || 0);
        this.setMetricValue('metric-queue-timeouts', metrics.queue_timeout_count || 0);
        this.setMetricValue('metric-avg-queue-time', this.formatDuration(metrics.average_queue_wait_time || 0));

        // Update timing statistics
        this.setMetricValue('metric-current-hold', this.formatDuration(metrics.current_hold_time || 0));
        this.setMetricValue('metric-avg-hold', this.formatDuration(metrics.average_hold_time || 0));
        this.setMetricValue('metric-max-hold', this.formatDuration(metrics.max_hold_time || 0));
        this.setMetricValue('metric-total-hold', this.formatDuration(metrics.total_hold_time || 0));

        // Update owner statistics
        this.setMetricValue('metric-owner-changes', metrics.owner_change_count || 0);
        this.setMetricValue('metric-unique-owners', metrics.unique_owners_count || 0);
        this.setMetricValue('metric-refresh-count', metrics.refresh_count || 0);
        this.setMetricValue('metric-heartbeat-count', metrics.heartbeat_count || 0);

        // Update error statistics
        this.setMetricValue('metric-failed-ops', metrics.failed_operations || 0, 'error-count');
        this.setMetricValue('metric-stale-tokens', metrics.stale_token_errors || 0, 'error-count');
        this.setMetricValue('metric-ttl-expirations', metrics.ttl_expiration_count || 0, 'error-count');
        this.setMetricValue('metric-max-ttl-expirations', metrics.max_ttl_expiration_count || 0, 'error-count');

        // Update timeline
        this.setMetricValue('metric-created', this.formatDate(metrics.created_at));
        this.setMetricValue('metric-last-activity', this.formatDate(metrics.last_activity_at));
        this.setMetricValue('metric-age', this.formatTimeSince(metrics.created_at));
        this.setMetricValue('metric-idle-time', this.formatTimeSince(metrics.last_activity_at));

        // Update owner history
        this.ownerHistoryData = metrics.owner_history || [];
        this.ownerHistoryPage = 1; // Reset to first page
        this.displayOwnerHistory();
    }

    setMetricValue(elementId, value, cssClass = '') {
        const element = document.getElementById(elementId);
        if (element) {
            element.textContent = value;
            element.className = `metric-value ${cssClass}`;
        }
    }

    formatDuration(nanoseconds) {
        if (!nanoseconds || nanoseconds === 0) return '0ms';

        const milliseconds = nanoseconds / 1000000;
        if (milliseconds < 1000) {
            return `${Math.round(milliseconds)}ms`;
        }

        const seconds = milliseconds / 1000;
        if (seconds < 60) {
            return `${Math.round(seconds)}s`;
        }

        const minutes = seconds / 60;
        if (minutes < 60) {
            return `${Math.round(minutes)}m`;
        }

        const hours = minutes / 60;
        return `${Math.round(hours)}h`;
    }

    formatDate(dateString) {
        if (!dateString) return 'Never';

        try {
            const date = new Date(dateString);
            return date.toLocaleString();
        } catch (e) {
            return 'Invalid Date';
        }
    }

    formatTimeSince(dateString) {
        if (!dateString) return 'Never';

        try {
            const date = new Date(dateString);
            const now = new Date();
            const diffMs = now - date;

            const seconds = Math.floor(diffMs / 1000);
            const minutes = Math.floor(seconds / 60);
            const hours = Math.floor(minutes / 60);
            const days = Math.floor(hours / 24);

            if (days > 0) return `${days}d`;
            if (hours > 0) return `${hours}h`;
            if (minutes > 0) return `${minutes}m`;
            return `${seconds}s`;
        } catch (e) {
            return 'Invalid';
        }
    }

    displayOwnerHistory() {
        const container = document.getElementById('owner-history-table');
        const paginationEl = document.getElementById('owner-history-pagination');

        if (!container) return;

        if (!this.ownerHistoryData || this.ownerHistoryData.length === 0) {
            container.innerHTML = '<div class="no-data">No ownership history available</div>';
            if (paginationEl) paginationEl.style.display = 'none';
            return;
        }

        // Sort by acquired_at descending (most recent first)
        const sortedHistory = [...this.ownerHistoryData].sort((a, b) =>
            new Date(b.acquired_at) - new Date(a.acquired_at)
        );

        // Calculate pagination
        const totalItems = sortedHistory.length;
        const totalPages = Math.ceil(totalItems / this.ownerHistoryPageSize);
        const startIndex = (this.ownerHistoryPage - 1) * this.ownerHistoryPageSize;
        const endIndex = Math.min(startIndex + this.ownerHistoryPageSize, totalItems);

        // Get current page items
        const currentPageItems = sortedHistory.slice(startIndex, endIndex);

        const html = currentPageItems.map(record => `
            <div class="owner-history-item">
                <div class="owner-info">
                    <div class="owner-name">${Utils.escapeHtml(record.owner || 'Unknown')}</div>
                    <div class="owner-id">${Utils.escapeHtml(record.owner_id || 'N/A')}</div>
                </div>
                <div class="owner-timing">
                    <div class="owner-acquired">Acquired: ${this.formatDate(record.acquired_at)}</div>
                    <div class="owner-duration">Duration: ${this.formatDuration(record.hold_time || 0)}</div>
                </div>
            </div>
        `).join('');

        container.innerHTML = html;

        // Update pagination controls
        this.updatePaginationControls(totalPages);
    }

    changeOwnerHistoryPage(direction) {
        const totalPages = Math.ceil(this.ownerHistoryData.length / this.ownerHistoryPageSize);
        const newPage = this.ownerHistoryPage + direction;

        if (newPage >= 1 && newPage <= totalPages) {
            this.ownerHistoryPage = newPage;
            this.displayOwnerHistory();
        }
    }

    updatePaginationControls(totalPages) {
        const paginationEl = document.getElementById('owner-history-pagination');
        const prevBtn = document.getElementById('owner-history-prev');
        const nextBtn = document.getElementById('owner-history-next');
        const pageInfo = document.getElementById('owner-history-page-info');

        if (!paginationEl || !prevBtn || !nextBtn || !pageInfo) return;

        if (totalPages <= 1) {
            paginationEl.style.display = 'none';
            return;
        }

        paginationEl.style.display = 'flex';
        pageInfo.textContent = `Page ${this.ownerHistoryPage} of ${totalPages}`;

        prevBtn.disabled = this.ownerHistoryPage <= 1;
        nextBtn.disabled = this.ownerHistoryPage >= totalPages;
    }

    showMetricsError(message) {
        const loadingEl = document.getElementById('metrics-loading');
        const contentEl = document.getElementById('metrics-content');

        if (loadingEl) {
            loadingEl.innerHTML = `
                <div style="text-align: center; color: #e74c3c; padding: 20px;">
                    <strong>Error loading metrics:</strong><br>
                    ${Utils.escapeHtml(message)}
                </div>
            `;
        }

        if (contentEl) contentEl.style.display = 'none';
    }
}

// Initialize when DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
    window.metricsManager = new MetricsManager();
});
