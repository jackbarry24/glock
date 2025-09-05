/**
 * Lock management functionality for the Glock Dashboard
 */

class LockManager {
    constructor() {
        this.locks = [];
        this.filteredLocks = [];
        this.searchTerm = '';
        this.statusFilter = '';
        this.lastUpdated = null;
        this.autoRefreshInterval = null;
        this.autoRefreshEnabled = false;

        this.init();
    }

    init() {
        this.bindEvents();
        // Small delay to ensure all components are initialized
        setTimeout(() => {
            this.loadLocks();
        }, 50);
    }

    bindEvents() {
        // Search and filter
        const searchInput = document.getElementById('search-input');
        const statusFilter = document.getElementById('status-filter');
        const refreshBtn = document.getElementById('refresh-btn');
        const autoRefreshCheckbox = document.getElementById('auto-refresh-checkbox');

        if (searchInput) {
            searchInput.addEventListener('input', Utils.debounce(() => {
                this.searchTerm = searchInput.value.toLowerCase();
                this.applyFilters();
            }, 300));
        }

        if (statusFilter) {
            statusFilter.addEventListener('change', () => {
                this.statusFilter = statusFilter.value;
                this.applyFilters();
            });
        }

        if (refreshBtn) {
            refreshBtn.addEventListener('click', () => this.loadLocks());
        }

        if (autoRefreshCheckbox) {
            autoRefreshCheckbox.addEventListener('change', (e) => {
                this.autoRefreshEnabled = e.target.checked;
                this.toggleAutoRefresh();
            });
        }

        // Modal close events
        document.addEventListener('click', (e) => {
            if (e.target.classList.contains('modal') || e.target.classList.contains('modal-close') || e.target.classList.contains('modal-cancel')) {
                this.closeAllModals();
            }
        });

        // Form submissions
        const createForm = document.getElementById('create-lock-form');
        const updateForm = document.getElementById('update-lock-form');

        if (createForm) {
            createForm.addEventListener('submit', (e) => this.handleCreateLock(e));
        }

        if (updateForm) {
            updateForm.addEventListener('submit', (e) => this.handleUpdateLock(e));
        }

        // Action button events (delegated)
        document.addEventListener('click', (e) => {
            if (e.target.classList.contains('update-btn')) {
                const lockName = e.target.dataset.lockName;
                this.showUpdateModal(lockName);
            } else if (e.target.classList.contains('delete-btn')) {
                const lockName = e.target.dataset.lockName;
                this.confirmDeleteLock(lockName);
            } else if (e.target.classList.contains('release-btn')) {
                const lockName = e.target.dataset.lockName;
                this.confirmReleaseLock(lockName);
            } else if (e.target.classList.contains('create-lock-btn')) {
                this.showCreateModal();
            } else if (e.target.classList.contains('freeze-btn')) {
                const lockName = e.target.dataset.lockName;
                this.freezeLock(lockName);
            } else if (e.target.classList.contains('unfreeze-btn')) {
                const lockName = e.target.dataset.lockName;
                this.unfreezeLock(lockName);
            } else if (e.target.classList.contains('queue-details-btn')) {
                const lockName = e.target.dataset.lockName;
                this.showQueueDetails(lockName);
            } else if (e.target.classList.contains('metrics-btn')) {
                const lockName = e.target.dataset.lockName;
                if (window.metricsManager) {
                    window.metricsManager.showMetricsModal(lockName);
                } else {
                    console.warn('MetricsManager not available yet, retrying in 100ms...');
                    setTimeout(() => {
                        if (window.metricsManager) {
                            window.metricsManager.showMetricsModal(lockName);
                        } else {
                            Utils.showAlert('Error', 'Metrics functionality is not available. Please refresh the page.', 'error');
                        }
                    }, 100);
                }
            }
        });
    }

    async loadLocks() {
        try {
            Utils.showLoading();
            const response = await fetch('/api/status');

            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }

            const data = await response.json();
            this.locks = data.locks || [];
            this.lastUpdated = new Date();

            this.updateStats();
            this.applyFilters();
            this.updateLastUpdated();

        } catch (error) {
            console.error('Error loading locks:', error);
            Utils.showAlert('Error', 'Failed to load locks: ' + error.message, 'error');
            this.showEmptyState();
        } finally {
            Utils.hideLoading();
        }
    }

    updateStats() {
        const totalLocks = this.locks.length;
        const availableLocks = this.locks.filter(lock => !lock.owner_id).length;
        const heldLocks = this.locks.filter(lock => lock.owner_id && this.isLockValid(lock)).length;
        const expiredLocks = this.locks.filter(lock => lock.owner_id && !this.isLockValid(lock)).length;

        document.getElementById('total-locks').textContent = totalLocks;
        document.getElementById('available-locks').textContent = availableLocks;
        document.getElementById('held-locks').textContent = heldLocks;
        document.getElementById('expired-locks').textContent = expiredLocks;
    }

    isLockValid(lock) {
        if (!lock.owner_id) return false;

        const now = new Date();
        const acquiredAt = new Date(lock.acquired_at);
        const lastRefresh = new Date(lock.last_refresh);
        const maxTTLExpiry = new Date(acquiredAt.getTime() + lock.max_ttl / 1000000);
        const ttlExpiry = new Date(lastRefresh.getTime() + lock.ttl / 1000000);

        return now <= maxTTLExpiry && now <= ttlExpiry;
    }

    applyFilters() {
        this.filteredLocks = this.locks.filter(lock => {
            // Search filter
            const matchesSearch = !this.searchTerm ||
                lock.name.toLowerCase().includes(this.searchTerm) ||
                (lock.owner || '').toLowerCase().includes(this.searchTerm) ||
                (lock.owner_id || '').toLowerCase().includes(this.searchTerm);

            // Status filter
            let matchesStatus = true;
            if (this.statusFilter) {
                switch (this.statusFilter) {
                    case 'available':
                        matchesStatus = !lock.owner_id;
                        break;
                    case 'held':
                        matchesStatus = lock.owner_id && this.isLockValid(lock);
                        break;
                    case 'expired':
                        matchesStatus = lock.owner_id && !this.isLockValid(lock);
                        break;
                }
            }

            return matchesSearch && matchesStatus;
        });

        this.renderLocks();
    }

    renderLocks() {
        const tbody = document.getElementById('locks-tbody');
        if (!tbody) return;

        if (this.filteredLocks.length === 0) {
            tbody.innerHTML = '<tr><td colspan="8" class="no-data">No locks found matching your criteria</td></tr>';
            return;
        }

        const rows = this.filteredLocks.map(lock => {
            const actionButtons = Utils.generateActionButtons(lock);
            return `
            <tr class="loading-row ${lock.frozen ? 'frozen-row' : ''}">
                <td><strong>${Utils.escapeHtml(lock.name)}</strong></td>
                <td>${Utils.formatOwner(lock)}</td>
                <td>${Utils.formatStatus(lock)}</td>
                <td>${Utils.formatTimeRemaining(lock)}</td>
                <td>${Utils.formatTTL(lock.ttl)}</td>
                <td>${Utils.formatTTL(lock.max_ttl)}</td>
                <td>${Utils.formatQueueInfo(lock)}</td>
                <td class="actions-column">${actionButtons}</td>
            </tr>
        `}).join('');

        tbody.innerHTML = rows;
    }

    showEmptyState() {
        const tbody = document.getElementById('locks-tbody');
        if (tbody) {
            tbody.innerHTML = '<tr><td colspan="8" class="no-data">Unable to load locks. Please try again.</td></tr>';
        }
    }

    updateLastUpdated() {
        const lastUpdatedEl = document.getElementById('last-updated');
        if (lastUpdatedEl) {
            lastUpdatedEl.textContent = 'Last updated: ' + Utils.formatLastUpdated(this.lastUpdated);
        }
    }

    toggleAutoRefresh() {
        if (this.autoRefreshEnabled) {
            this.autoRefreshInterval = setInterval(() => {
                this.loadLocks();
            }, 5000); // 5 seconds
        } else {
            if (this.autoRefreshInterval) {
                clearInterval(this.autoRefreshInterval);
                this.autoRefreshInterval = null;
            }
        }
    }

    showCreateModal() {
        const modal = document.getElementById('create-modal');
        if (modal) {
            // Reset form
            const form = document.getElementById('create-lock-form');
            if (form) {
                form.reset();
            }
            modal.style.display = 'flex';
        }
    }

    showUpdateModal(lockName) {
        const lock = this.locks.find(l => l.name === lockName);
        if (!lock) return;

        const modal = document.getElementById('update-modal');
        if (modal) {
            // Populate form
            document.getElementById('update-lock-name').value = lock.name;
            document.getElementById('update-ttl').value = this.secondsToDuration(lock.ttl);
            document.getElementById('update-max-ttl').value = this.secondsToDuration(lock.max_ttl);
            document.getElementById('update-queue-type').value = lock.queue_type;
            document.getElementById('update-queue-timeout').value = this.secondsToDuration(lock.queue_timeout);
            document.getElementById('update-metadata').value = Utils.formatMetadata(lock.metadata);

            modal.style.display = 'flex';
        }
    }

    secondsToDuration(nanoseconds) {
        // Convert nanoseconds to seconds
        const seconds = nanoseconds / 1000000000;
        if (seconds >= 86400) return Math.floor(seconds / 86400) + 'd';
        if (seconds >= 3600) return Math.floor(seconds / 3600) + 'h';
        if (seconds >= 60) return Math.floor(seconds / 60) + 'm';
        return seconds + 's';
    }

    async handleCreateLock(e) {
        e.preventDefault();

        const formData = new FormData(e.target);
        const lockData = {
            name: formData.get('name'),
            ttl: formData.get('ttl'),
            max_ttl: formData.get('max_ttl'),
            queue_type: formData.get('queue_type') || 'none',
            queue_timeout: formData.get('queue_timeout') || '',
            metadata: formData.get('metadata') || null
        };

        // Parse metadata if provided
        if (lockData.metadata) {
            lockData.metadata = Utils.safeJsonParse(lockData.metadata);
            if (lockData.metadata === null) {
                Utils.showAlert('Error', 'Invalid JSON in metadata field', 'error');
                return;
            }
        }

        try {
            const response = await fetch('/api/create', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(lockData)
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            this.closeAllModals();
            Utils.showAlert('Success', 'Lock created successfully!', 'success');
            this.loadLocks();

        } catch (error) {
            console.error('Error creating lock:', error);
            Utils.showAlert('Error', 'Failed to create lock: ' + error.message, 'error');
        }
    }

    async handleUpdateLock(e) {
        e.preventDefault();

        const formData = new FormData(e.target);
        const lockData = {
            name: formData.get('name'),
            ttl: formData.get('ttl'),
            max_ttl: formData.get('max_ttl'),
            queue_type: formData.get('queue_type') || 'none',
            queue_timeout: formData.get('queue_timeout') || '',
            metadata: formData.get('metadata') || null
        };

        // Parse metadata if provided
        if (lockData.metadata) {
            lockData.metadata = Utils.safeJsonParse(lockData.metadata);
            if (lockData.metadata === null) {
                Utils.showAlert('Error', 'Invalid JSON in metadata field', 'error');
                return;
            }
        }

        try {
            const response = await fetch('/api/update', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(lockData)
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            this.closeAllModals();
            Utils.showAlert('Success', 'Lock updated successfully!', 'success');
            this.loadLocks();

        } catch (error) {
            console.error('Error updating lock:', error);
            Utils.showAlert('Error', 'Failed to update lock: ' + error.message, 'error');
        }
    }

    confirmDeleteLock(lockName) {
        Utils.showConfirm(
            'Delete Lock',
            `Are you sure you want to delete the lock "${lockName}"?`,
            () => this.deleteLock(lockName)
        );
    }

    async deleteLock(lockName) {
        try {
            const response = await fetch(`/api/delete/${encodeURIComponent(lockName)}`, {
                method: 'DELETE'
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            Utils.showAlert('Success', 'Lock deleted successfully!', 'success');
            this.loadLocks();

        } catch (error) {
            console.error('Error deleting lock:', error);
            Utils.showAlert('Error', 'Failed to delete lock: ' + error.message, 'error');
        }
    }

    confirmReleaseLock(lockName) {
        Utils.showConfirm(
            'Release Lock',
            `Are you sure you want to release the lock "${lockName}"?`,
            () => this.releaseLock(lockName)
        );
    }

    async releaseLock(lockName) {
        // We need to find the lock to get the owner_id and token
        const lock = this.locks.find(l => l.name === lockName);
        if (!lock || !lock.owner_id) {
            Utils.showAlert('Error', 'Lock is not currently held', 'error');
            return;
        }

        // For demo purposes, we'll use placeholder values since we don't have the real token
        // In a real implementation, you'd need to get this from the lock data
        const releaseData = {
            name: lockName,
            owner_id: lock.owner_id,
            token: lock.token || 1 // This would need to be the actual token
        };

        try {
            const response = await fetch('/api/release', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(releaseData)
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            Utils.showAlert('Success', 'Lock released successfully!', 'success');
            this.loadLocks();

        } catch (error) {
            console.error('Error releasing lock:', error);
            Utils.showAlert('Error', 'Failed to release lock: ' + error.message, 'error');
        }
    }

    async showQueueDetails(lockName) {
        const lock = this.locks.find(l => l.name === lockName);
        if (!lock || lock.queue_type === 'none') {
            Utils.showAlert('Info', 'This lock has no queue configured.', 'info');
            return;
        }

        try {
            // Show loading
            Utils.showLoading();

            // Fetch queue details from API
            const response = await fetch('/api/queue/list', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ name: lockName })
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            const data = await response.json();
            this.showQueueDetailsModal(lock, data.requests);

        } catch (error) {
            console.error('Error loading queue details:', error);
            Utils.showAlert('Error', 'Failed to load queue details: ' + error.message, 'error');
        } finally {
            Utils.hideLoading();
        }
    }

    showQueueDetailsModal(lock, requests) {
        // Check if modal already exists and remove it
        const existingModal = document.querySelector('.modal');
        if (existingModal) {
            existingModal.remove();
        }

        // Create queue details modal content
        const modal = document.createElement('div');
        modal.className = 'modal';
        modal.innerHTML = `
            <div class="modal-content" style="max-width: 800px;">
                <div class="modal-header">
                    <h3>Queue Details - ${lock.name}</h3>
                    <span class="modal-close">&times;</span>
                </div>
                <div class="modal-body">
                    <div class="queue-summary">
                        <p><strong>Queue Type:</strong> ${lock.queue_type.toUpperCase()}</p>
                        <p><strong>Requests in Queue:</strong> ${requests.length}</p>
                        <p><strong>Queue Timeout:</strong> ${Utils.formatTTL(lock.queue_timeout)}</p>
                    </div>

                    ${requests.length > 0 ? this.generateQueueTable(requests) : '<div class="no-data">Queue is currently empty</div>'}
                </div>
                <div class="modal-actions">
                    <button type="button" class="btn btn-secondary" id="refresh-queue-btn">Refresh</button>
                    <button type="button" class="btn btn-primary modal-close">Close</button>
                </div>
            </div>
        `;

        document.body.appendChild(modal);
        modal.style.display = 'flex';

        // Store references for refresh functionality
        modal._lockName = lock.name;
        modal._lock = lock;

        // Add event listeners
        const closeBtn = modal.querySelector('.modal-close');
        const closeActionBtn = modal.querySelector('.modal-actions .btn-primary');
        const refreshBtn = modal.querySelector('#refresh-queue-btn');

        const closeModal = () => {
            if (modal.parentNode) {
                modal.parentNode.removeChild(modal);
            }
        };

        const refreshQueue = async () => {
            try {
                Utils.showLoading();
                const response = await fetch('/api/queue/list', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                    },
                    body: JSON.stringify({ name: modal._lockName })
                });

                if (!response.ok) {
                    const error = await response.json();
                    throw new Error(error.error || `HTTP ${response.status}`);
                }

                const data = await response.json();

                // Update the queue table
                const modalBody = modal.querySelector('.modal-body');
                const newContent = `
                    <div class="queue-summary">
                        <p><strong>Queue Type:</strong> ${modal._lock.queue_type.toUpperCase()}</p>
                        <p><strong>Requests in Queue:</strong> ${data.requests.length}</p>
                        <p><strong>Queue Timeout:</strong> ${Utils.formatTTL(modal._lock.queue_timeout)}</p>
                    </div>
                    ${data.requests.length > 0 ? this.generateQueueTable(data.requests) : '<div class="no-data">Queue is currently empty</div>'}
                `;
                modalBody.innerHTML = newContent;

                Utils.showAlert('Success', 'Queue details refreshed!', 'success');

            } catch (error) {
                console.error('Error refreshing queue:', error);
                Utils.showAlert('Error', 'Failed to refresh queue: ' + error.message, 'error');
            } finally {
                Utils.hideLoading();
            }
        };

        closeBtn.addEventListener('click', closeModal);
        closeActionBtn.addEventListener('click', closeModal);
        refreshBtn.addEventListener('click', refreshQueue);

        // Add remove button event listeners
        modal.addEventListener('click', async (e) => {
            if (e.target.classList.contains('remove-queue-btn')) {
                e.preventDefault();
                const requestId = e.target.dataset.requestId;
                const ownerId = e.target.dataset.ownerId;
                await this.removeFromQueue(lock.name, requestId, ownerId, modal);
            } else if (e.target === modal) {
                closeModal();
            }
        });
    }

    generateQueueTable(requests) {
        return `
            <div class="queue-table-container">
                <table class="queue-table">
                    <thead>
                        <tr>
                            <th class="position-col">Position</th>
                            <th class="request-id-col">Request ID</th>
                            <th class="owner-col">Owner</th>
                            <th class="time-col">Queued At</th>
                            <th class="time-col">Time in Queue</th>
                            <th class="actions-col">Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${requests.map((req, index) => `
                            <tr>
                                <td class="position-col">${index + 1}</td>
                                <td class="request-id-col"><code>${Utils.escapeHtml(req.id)}</code></td>
                                <td class="owner-col">${Utils.escapeHtml(req.owner)}</td>
                                <td class="time-col">${new Date(req.queued_at).toLocaleString()}</td>
                                <td class="time-col">${this.formatTimeInQueue(req.queued_at)}</td>
                                <td class="actions-col">
                                    <button class="remove-queue-btn"
                                            data-request-id="${req.id}"
                                            data-owner-id="${req.owner_id}"
                                            title="Remove from queue">Remove</button>
                                </td>
                            </tr>
                        `).join('')}
                    </tbody>
                </table>
            </div>
        `;
    }

    formatTimeInQueue(queuedAt) {
        const now = new Date();
        const queued = new Date(queuedAt);
        const diffMs = now - queued;
        const diffSeconds = Math.floor(diffMs / 1000);

        if (diffSeconds < 60) return `${diffSeconds}s`;
        if (diffSeconds < 3600) return `${Math.floor(diffSeconds / 60)}m`;
        return `${Math.floor(diffSeconds / 3600)}h`;
    }

    async removeFromQueue(lockName, requestId, ownerId, modal) {
        Utils.showConfirm(
            'Remove from Queue',
            `Are you sure you want to remove request "${requestId}" from the queue?`,
            async () => {
                try {
                    Utils.showLoading();

                    const response = await fetch('/api/remove', {
                        method: 'POST',
                        headers: {
                            'Content-Type': 'application/json',
                        },
                        body: JSON.stringify({
                            name: lockName,
                            request_id: requestId,
                            owner_id: ownerId
                        })
                    });

                    if (!response.ok) {
                        const error = await response.json();
                        throw new Error(error.error || `HTTP ${response.status}`);
                    }

                    const result = await response.json();

                    if (result.removed) {
                        Utils.showAlert('Success', 'Request removed from queue successfully!', 'success');

                        // Refresh the queue details
                        const refreshResponse = await fetch('/api/queue/list', {
                            method: 'POST',
                            headers: {
                                'Content-Type': 'application/json',
                            },
                            body: JSON.stringify({ name: lockName })
                        });

                        if (refreshResponse.ok) {
                            const refreshData = await refreshResponse.json();

                            // Update the modal content
                            const modalBody = modal.querySelector('.modal-body');
                            const newContent = `
                                <div class="queue-summary">
                                    <p><strong>Queue Type:</strong> ${modal._lock.queue_type.toUpperCase()}</p>
                                    <p><strong>Requests in Queue:</strong> ${refreshData.requests.length}</p>
                                    <p><strong>Queue Timeout:</strong> ${Utils.formatTTL(modal._lock.queue_timeout)}</p>
                                </div>
                                ${refreshData.requests.length > 0 ? this.generateQueueTable(refreshData.requests) : '<div class="no-data">Queue is currently empty</div>'}
                            `;
                            modalBody.innerHTML = newContent;
                        }

                        // Refresh the main locks list to update queue sizes
                        this.loadLocks();
                    } else {
                        Utils.showAlert('Warning', 'Request was not found in queue', 'warning');
                    }

                } catch (error) {
                    console.error('Error removing from queue:', error);
                    Utils.showAlert('Error', 'Failed to remove request from queue: ' + error.message, 'error');
                } finally {
                    Utils.hideLoading();
                }
            }
        );
    }

    async freezeLock(lockName) {
        try {
            const response = await fetch(`/api/freeze/${encodeURIComponent(lockName)}`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                }
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            const result = await response.json();
            Utils.showAlert('Success', result.message || 'Lock frozen successfully!', 'success');
            this.loadLocks();

        } catch (error) {
            console.error('Error freezing lock:', error);
            Utils.showAlert('Error', 'Failed to freeze lock: ' + error.message, 'error');
        }
    }

    async unfreezeLock(lockName) {
        try {
            const response = await fetch(`/api/unfreeze/${encodeURIComponent(lockName)}`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                }
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            const result = await response.json();
            Utils.showAlert('Success', result.message || 'Lock unfrozen successfully!', 'success');
            this.loadLocks();

        } catch (error) {
            console.error('Error unfreezing lock:', error);
            Utils.showAlert('Error', 'Failed to unfreeze lock: ' + error.message, 'error');
        }
    }

    closeAllModals() {
        const modals = document.querySelectorAll('.modal');
        modals.forEach(modal => {
            modal.style.display = 'none';
        });
    }
}

// Initialize when DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
    window.lockManager = new LockManager();
});
