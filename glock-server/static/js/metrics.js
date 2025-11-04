/**
 * Metrics functionality for the Glock Dashboard
 */

class MetricsManager {
    constructor() {
        this.currentLockName = null;
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
    }

    async loadMetrics(lockName) {
        const loadingEl = document.getElementById('metrics-loading');
        const contentEl = document.getElementById('metrics-content');

        try {
            if (loadingEl) loadingEl.style.display = 'block';
            if (contentEl) contentEl.style.display = 'none';

            const response = await fetch(`/api/locks/${encodeURIComponent(lockName)}/metrics`);

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            const data = await response.json();
            this.displayMetrics(data.metrics, lockName);

        } catch (error) {
            console.error('Error loading metrics:', error);
            this.showMetricsError(error.message);
        } finally {
            if (loadingEl) loadingEl.style.display = 'none';
            if (contentEl) contentEl.style.display = 'block';
        }
    }

    displayMetrics(metrics, lockName) {
        if (!metrics) {
            this.showMetricsError('No metrics data available');
            return;
        }

        // Update usage statistics
        this.setMetricValue('metric-total-attempts', metrics.total_acquires || 0);
        this.setMetricValue('metric-successful', metrics.success_acquires || 0);
        this.setMetricValue('metric-failed', metrics.failed_acquires || 0);

        const totalAttempts = metrics.total_acquires || 0;
        const successful = metrics.success_acquires || 0;
        const successRate = totalAttempts > 0 ? Math.round((successful / totalAttempts) * 100) : 0;
        this.setMetricValue('metric-success-rate', `${successRate}%`, 'success-rate');

        // Update queue statistics
        this.setMetricValue('metric-current-queue', metrics.current_queue_len || 0);
        this.setMetricValue('metric-total-queued', metrics.total_queued || 0);
        this.setMetricValue('metric-queue-timeouts', metrics.queue_timeouts || 0);
        this.setMetricValue('metric-avg-queue-time', this.formatDuration(metrics.avg_queue_time_ns || 0));

        // Update timing statistics
        this.setMetricValue('metric-current-hold', this.formatDuration(metrics.current_hold_time_ns || 0));
        this.setMetricValue('metric-avg-hold', this.formatDuration(metrics.avg_hold_time_ns || 0));
        this.setMetricValue('metric-max-hold', this.formatDuration(metrics.max_hold_time_ns || 0));
        this.setMetricValue('metric-total-hold', this.formatDuration(metrics.total_hold_time_ns || 0));

        // Update owner statistics
        this.setMetricValue('metric-owner-changes', metrics.owner_changes || 0);
        this.setMetricValue('metric-unique-owners', metrics.unique_owners || 0);
        this.setMetricValue('metric-refresh-count', metrics.total_refreshes || 0);
        this.setMetricValue('metric-heartbeat-count', 'N/A');

        // Update error statistics
        this.setMetricValue('metric-failed-ops', metrics.failed_acquires || 0, 'error-count');
        this.setMetricValue('metric-stale-tokens', 'N/A', 'error-count');
        this.setMetricValue('metric-ttl-expirations', metrics.ttl_expirations || 0, 'error-count');
        this.setMetricValue('metric-max-ttl-expirations', 'N/A', 'error-count');

        // Update timeline
        this.setMetricValue('metric-created', this.formatDate(metrics.created_at));
        this.setMetricValue('metric-last-activity', this.formatDate(metrics.last_activity_at));
        this.setMetricValue('metric-age', this.formatTimeSince(metrics.created_at));
        this.setMetricValue('metric-idle-time', this.formatTimeSince(metrics.last_activity_at));
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
