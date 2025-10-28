/**
 * Lock management functionality for the Glock Dashboard
 */

class LockManager {
    constructor() {
        this.locks = [];
        this.filteredLocks = [];
        this.searchTerm = '';
        this.statusFilter = '';
        this.tagFilter = '';
        this.lastUpdated = null;
        this.autoRefreshInterval = null;
        this.autoRefreshEnabled = false;
        this.lockHierarchy = {};

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
        const tagFilter = document.getElementById('tag-filter');
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

        if (tagFilter) {
            tagFilter.addEventListener('change', () => {
                this.tagFilter = tagFilter.value;
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
        const releaseForm = document.getElementById('release-lock-form');

        if (createForm) {
            createForm.addEventListener('submit', (e) => this.handleCreateLock(e));
        }

        if (releaseForm) {
            releaseForm.addEventListener('submit', (e) => this.handleReleaseLock(e));
        }

        // Action button events (delegated)
        document.addEventListener('click', (e) => {
            if (e.target.classList.contains('delete-btn')) {
                const lockName = e.target.dataset.lockName;
                this.confirmDeleteLock(lockName);
            } else if (e.target.classList.contains('release-btn')) {
                const lockName = e.target.dataset.lockName;
                this.confirmReleaseLock(lockName);
            } else if (e.target.classList.contains('acquire-btn')) {
                const lockName = e.target.dataset.lockName;
                this.acquireLock(lockName);
            } else if (e.target.classList.contains('create-lock-btn')) {
                this.showCreateModal();
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
            const response = await fetch('/api/locks');

            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }

            const lockIds = await response.json();
            const locks = lockIds.locks || [];

            // Fetch status for each lock
            const statusPromises = locks.map(lockId =>
                fetch(`/api/locks/${encodeURIComponent(lockId)}`)
                    .then(r => r.ok ? r.json() : null)
                    .then(data => data ? { lockId, status: data.status } : null)
            );

            const statuses = (await Promise.all(statusPromises)).filter(s => s !== null);

            this.locks = statuses.map(({ lockId, status }) => this.transformLockData(status, lockId));
            this.lastUpdated = new Date();

            this.buildLockHierarchy();
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

    transformLockData(status, lockID) {
        const lock = {
            name: lockID,
            owner_id: status.owner ? status.owner.client_id : null,
            owner: status.owner ? status.owner.client_id : null,
            acquired_at: status.owner ? status.owner.acquired_at : null,
            last_refresh: status.owner ? status.owner.acquired_at : null,
            ttl: status.config.default_ttl,
            max_ttl: status.config.max_ttl,
            parent: '',
            queue_type: status.config.queue_type || 'none',
            queue_size: status.queue_len,
            queue_timeout: 300000000000,
            token: status.owner ? status.owner.lease_id : null,
            fence_token: status.owner ? status.owner.fence_token : null,
            metadata: null,
            frozen: false,
            expires_at: status.owner ? status.owner.expires_at : null,
            tags: status.tags || []
        };
        return lock;
    }

    updateStats() {
        const totalLocks = this.locks.length;
        const blockedLocks = this.locks.filter(lock => {
            const ancestorInfo = this.lockHierarchy[lock.name] || { isBlocked: false };
            return ancestorInfo.isBlocked;
        }).length;
        const availableLocks = this.locks.filter(lock => {
            const ancestorInfo = this.lockHierarchy[lock.name] || { isBlocked: false };
            return !lock.owner_id && !ancestorInfo.isBlocked;
        }).length;
        const heldLocks = this.locks.filter(lock => lock.owner_id && this.isLockValid(lock)).length;
        const expiredLocks = this.locks.filter(lock => lock.owner_id && !this.isLockValid(lock)).length;

        document.getElementById('total-locks').textContent = totalLocks;
        document.getElementById('available-locks').textContent = availableLocks;
        document.getElementById('held-locks').textContent = heldLocks;
        const blockedLocksEl = document.getElementById('blocked-locks');
        if (blockedLocksEl) {
            blockedLocksEl.textContent = blockedLocks;
        }
        document.getElementById('expired-locks').textContent = expiredLocks;
    }

    buildLockHierarchy() {
        // Build a map of lock names to their locks for quick lookup
        const lockMap = {};
        this.locks.forEach(lock => {
            lockMap[lock.name] = lock;
        });

        // For each lock, compute if any ancestor is held
        this.lockHierarchy = {};
        this.locks.forEach(lock => {
            const ancestorInfo = this.findHeldAncestor(lock, lockMap);
            this.lockHierarchy[lock.name] = ancestorInfo;
        });
    }

    findHeldAncestor(lock, lockMap) {
        // Walk up the parent chain to find if any ancestor is held
        let current = lock;
        while (current.parent && current.parent !== '') {
            const parent = lockMap[current.parent];
            if (!parent) break; // Parent doesn't exist

            // Check if parent is held
            if (parent.owner_id && this.isLockValid(parent)) {
                return {
                    isBlocked: true,
                    blockedBy: parent.name,
                    blockedByOwner: parent.owner || parent.owner_id
                };
            }
            current = parent;
        }
        return { isBlocked: false };
    }

    isLockValid(lock) {
        if (!lock.owner_id || !lock.expires_at) return false;

        const now = new Date();
        const expiresAt = new Date(lock.expires_at);

        return now <= expiresAt;
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
                const ancestorInfo = this.lockHierarchy[lock.name] || { isBlocked: false };
                switch (this.statusFilter) {
                    case 'available':
                        matchesStatus = !lock.owner_id && !ancestorInfo.isBlocked;
                        break;
                    case 'held':
                        matchesStatus = lock.owner_id && this.isLockValid(lock);
                        break;
                    case 'expired':
                        matchesStatus = lock.owner_id && !this.isLockValid(lock);
                        break;
                    case 'blocked':
                        matchesStatus = ancestorInfo.isBlocked;
                        break;
                }
            }

            // Tag filter
            let matchesTag = true;
            if (this.tagFilter) {
                matchesTag = lock.tags && lock.tags.includes(this.tagFilter);
            }

            return matchesSearch && matchesStatus && matchesTag;
        });

        this.updateTagFilterOptions();
        this.renderLocks();
    }

    updateTagFilterOptions() {
        const tagFilter = document.getElementById('tag-filter');
        if (!tagFilter) return;

        const allTags = new Set();
        this.locks.forEach(lock => {
            if (lock.tags) {
                lock.tags.forEach(tag => allTags.add(tag));
            }
        });

        const currentValue = tagFilter.value;
        const sortedTags = Array.from(allTags).sort();

        tagFilter.innerHTML = '<option value="">All Tags</option>';
        sortedTags.forEach(tag => {
            const option = document.createElement('option');
            option.value = tag;
            option.textContent = tag;
            if (tag === currentValue) {
                option.selected = true;
            }
            tagFilter.appendChild(option);
        });
    }

    renderLocks() {
        const tbody = document.getElementById('locks-tbody');
        if (!tbody) return;

        if (this.filteredLocks.length === 0) {
            tbody.innerHTML = '<tr><td colspan="9" class="no-data">No locks found matching your criteria</td></tr>';
            return;
        }

        const rows = this.filteredLocks.map(lock => {
            const ancestorInfo = this.lockHierarchy[lock.name] || { isBlocked: false };
            const actionButtons = Utils.generateActionButtons(lock, ancestorInfo);
            const rowClass = lock.frozen ? 'frozen-row' : (ancestorInfo.isBlocked ? 'blocked-row' : '');
            return `
            <tr class="loading-row ${rowClass}">
                <td><strong>${Utils.escapeHtml(lock.name)}</strong>${this.formatParentInfo(lock)}</td>
                <td>${Utils.formatOwner(lock, ancestorInfo)}</td>
                <td>${Utils.formatStatus(lock, ancestorInfo)}</td>
                <td>${Utils.formatTimeRemaining(lock)}</td>
                <td>${Utils.formatTTL(lock.ttl)}</td>
                <td>${Utils.formatTTL(lock.max_ttl)}</td>
                <td>${Utils.formatQueueInfo(lock)}</td>
                <td>${this.formatTags(lock.tags)}</td>
                <td class="actions-column">${actionButtons}</td>
            </tr>
        `}).join('');

        tbody.innerHTML = rows;
    }

    formatTags(tags) {
        if (!tags || tags.length === 0) {
            return '<span style="color: #999;">—</span>';
        }

        const maxVisible = 3;
        const visibleTags = tags.slice(0, maxVisible);
        const remaining = tags.length - maxVisible;

        let html = '<div class="tags-container">';
        visibleTags.forEach(tag => {
            html += `<span class="tag">${Utils.escapeHtml(tag)}</span>`;
        });

        if (remaining > 0) {
            const allTagsEscaped = tags.map(t => Utils.escapeHtml(t)).join(', ');
            html += `<span class="tag tag-more" title="${allTagsEscaped}">+${remaining}</span>`;
        }

        html += '</div>';
        return html;
    }

    showEmptyState() {
        const tbody = document.getElementById('locks-tbody');
        if (tbody) {
            tbody.innerHTML = '<tr><td colspan="9" class="no-data">Unable to load locks. Please try again.</td></tr>';
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

            // Set up queue type change handler (do this each time to ensure it's attached)
            const queueTypeSelect = document.getElementById('create-queue-type');
            if (queueTypeSelect) {
                // Remove any existing listener by cloning the element
                const newQueueTypeSelect = queueTypeSelect.cloneNode(true);
                queueTypeSelect.parentNode.replaceChild(newQueueTypeSelect, queueTypeSelect);

                // Add the event listener
                newQueueTypeSelect.addEventListener('change', (e) => {
                    this.toggleQueueConfigFields(e.target.value);
                });

                // Initialize visibility based on current value
                this.toggleQueueConfigFields(newQueueTypeSelect.value);
            }
        }
    }

    toggleQueueConfigFields(queueType) {
        const queueConfigFields = document.getElementById('queue-config-fields');
        if (queueConfigFields) {
            if (queueType === 'none') {
                queueConfigFields.style.display = 'none';
            } else {
                queueConfigFields.style.display = 'flex';
            }
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
        const queueType = formData.get('queue_type') || 'none';
        const maxQueue = formData.get('max_queue');
        const queueTimeout = formData.get('queue_timeout');
        const tagsInput = formData.get('tags');

        const config = {
            default_ttl: formData.get('ttl'),
            max_ttl: formData.get('max_ttl'),
            max_queue: maxQueue ? parseInt(maxQueue, 10) : (queueType === 'none' ? 0 : 100),
            queue_type: queueType
        };

        if (queueTimeout && queueTimeout.trim() !== '') {
            config.default_timeout = queueTimeout;
        }

        const lockData = {
            lock_id: formData.get('name'),
            config: config
        };

        if (tagsInput && tagsInput.trim() !== '') {
            lockData.tags = tagsInput.split(',')
                .map(tag => tag.trim())
                .filter(tag => tag.length > 0)
                .slice(0, 16);
        }

        try {
            const response = await fetch('/api/locks', {
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


    confirmDeleteLock(lockName) {
        Utils.showConfirm(
            'Delete Lock',
            `Are you sure you want to delete the lock "${lockName}"?`,
            () => this.deleteLock(lockName)
        );
    }

    async deleteLock(lockName) {
        try {
            const response = await fetch(`/api/locks/${encodeURIComponent(lockName)}`, {
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
        this.showReleaseModal(lockName);
    }

    showReleaseModal(lockName) {
        const lock = this.locks.find(l => l.name === lockName);
        if (!lock || !lock.owner_id) {
            Utils.showAlert('Error', 'Lock is not currently held', 'error');
            return;
        }

        const modal = document.getElementById('release-modal');
        if (modal) {
            // Reset form
            const form = document.getElementById('release-lock-form');
            if (form) {
                form.reset();
                document.getElementById('release-lock-name').value = lockName;
                document.getElementById('release-lock-name-display').value = lockName;
            }
            modal.style.display = 'flex';
        }
    }

    async handleReleaseLock(e) {
        e.preventDefault();

        const formData = new FormData(e.target);
        const lockName = formData.get('lock_name');
        const clientId = formData.get('client_id');
        const leaseId = formData.get('lease_id');

        try {
            Utils.showLoading();

            const releaseData = {
                client_id: clientId,
                lease_id: leaseId
            };

            const response = await fetch(`/api/locks/${encodeURIComponent(lockName)}/lease`, {
                method: 'DELETE',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(releaseData)
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            this.closeAllModals();
            Utils.showAlert('Success', 'Lock released successfully!', 'success');
            this.loadLocks();

        } catch (error) {
            console.error('Error releasing lock:', error);
            Utils.showAlert('Error', 'Failed to release lock: ' + error.message, 'error');
        } finally {
            Utils.hideLoading();
        }
    }

    async showQueueDetails(lockName) {
        const lock = this.locks.find(l => l.name === lockName);
        if (!lock || lock.queue_type === 'none') {
            Utils.showAlert('Info', 'This lock has no queue configured.', 'info');
            return;
        }

        try {
            Utils.showLoading();

            const response = await fetch(`/api/locks/${encodeURIComponent(lockName)}/queue?limit=100`);

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            const data = await response.json();
            this.showQueueDetailsModal(lock, data.queue);

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
                const response = await fetch(`/api/locks/${encodeURIComponent(modal._lockName)}/queue?limit=100`);

                if (!response.ok) {
                    const error = await response.json();
                    throw new Error(error.error || `HTTP ${response.status}`);
                }

                const data = await response.json();

                const modalBody = modal.querySelector('.modal-body');
                const newContent = `
                    <div class="queue-summary">
                        <p><strong>Queue Type:</strong> ${modal._lock.queue_type.toUpperCase()}</p>
                        <p><strong>Requests in Queue:</strong> ${data.queue.length}</p>
                        <p><strong>Queue Timeout:</strong> ${Utils.formatTTL(modal._lock.queue_timeout)}</p>
                    </div>
                    ${data.queue.length > 0 ? this.generateQueueTable(data.queue) : '<div class="no-data">Queue is currently empty</div>'}
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

        modal.addEventListener('click', async (e) => {
            if (e.target.classList.contains('remove-queue-btn')) {
                e.preventDefault();
                const waitId = e.target.dataset.waitId;
                const clientId = e.target.dataset.clientId;
                await this.removeFromQueue(lock.name, waitId, clientId, modal);
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
                            <th class="request-id-col">Wait ID</th>
                            <th class="owner-col">Client ID</th>
                            <th class="time-col">Timeout At</th>
                            <th class="actions-col">Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${requests.map((req, index) => `
                            <tr>
                                <td class="position-col">${req.position}</td>
                                <td class="request-id-col"><code>${Utils.escapeHtml(req.wait_id)}</code></td>
                                <td class="owner-col">${Utils.escapeHtml(req.client_id)}</td>
                                <td class="time-col">${new Date(req.timeout_at).toLocaleString()}</td>
                                <td class="actions-col">
                                    <button class="remove-queue-btn"
                                            data-wait-id="${req.wait_id}"
                                            data-client-id="${req.client_id}"
                                            title="Remove from queue">Remove</button>
                                </td>
                            </tr>
                        `).join('')}
                    </tbody>
                </table>
            </div>
        `;
    }


    async removeFromQueue(lockName, waitId, clientId, modal) {
        Utils.showConfirm(
            'Remove from Queue',
            `Are you sure you want to remove wait entry "${waitId}" from the queue?`,
            async () => {
                try {
                    Utils.showLoading();

                    const response = await fetch(`/api/locks/${encodeURIComponent(lockName)}/queue/${encodeURIComponent(waitId)}`, {
                        method: 'DELETE',
                        headers: {
                            'Content-Type': 'application/json',
                        },
                        body: JSON.stringify({
                            client_id: clientId
                        })
                    });

                    if (!response.ok) {
                        const error = await response.json();
                        throw new Error(error.error || `HTTP ${response.status}`);
                    }

                    const result = await response.json();

                    if (result.removed) {
                        Utils.showAlert('Success', 'Request removed from queue successfully!', 'success');

                        const refreshResponse = await fetch(`/api/locks/${encodeURIComponent(lockName)}/queue?limit=100`);

                        if (refreshResponse.ok) {
                            const refreshData = await refreshResponse.json();

                            const modalBody = modal.querySelector('.modal-body');
                            const newContent = `
                                <div class="queue-summary">
                                    <p><strong>Queue Type:</strong> ${modal._lock.queue_type.toUpperCase()}</p>
                                    <p><strong>Requests in Queue:</strong> ${refreshData.queue.length}</p>
                                    <p><strong>Queue Timeout:</strong> ${Utils.formatTTL(modal._lock.queue_timeout)}</p>
                                </div>
                                ${refreshData.queue.length > 0 ? this.generateQueueTable(refreshData.queue) : '<div class="no-data">Queue is currently empty</div>'}
                            `;
                            modalBody.innerHTML = newContent;
                        }

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


    async acquireLock(lockName) {
        const clientId = Utils.generateUUID();

        try {
            Utils.showLoading();

            const acquireData = {
                client_id: clientId,
                lease_ttl: '30s',
                wait: false
            };

            const response = await fetch(`/api/locks/${encodeURIComponent(lockName)}/lease`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify(acquireData)
            });

            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || `HTTP ${response.status}`);
            }

            const result = await response.json();

            if (result.state === 'granted') {
                this.showAcquireSuccessModal(lockName, clientId, result.lease_id, result.fence_token);
                this.loadLocks();
            } else {
                throw new Error(`Lock acquisition failed: ${result.state}`);
            }

        } catch (error) {
            console.error('Error acquiring lock:', error);
            Utils.showAlert('Error', 'Failed to acquire lock: ' + error.message, 'error');
        } finally {
            Utils.hideLoading();
        }
    }

    showAcquireSuccessModal(lockName, clientId, leaseId, fenceToken) {
        const modal = document.createElement('div');
        modal.className = 'modal';
        modal.innerHTML = `
            <div class="modal-content">
                <div class="modal-header">
                    <h3>Lock Acquired</h3>
                    <span class="modal-close">&times;</span>
                </div>
                <div class="modal-body" style="padding: 20px;">
                    <p style="margin: 0 0 15px 0; color: #666;">Lock <strong>${Utils.escapeHtml(lockName)}</strong> has been acquired.</p>
                    <p style="margin: 10px 0 5px 0; font-weight: 500;">Client ID:</p>
                    <div style="display: flex; gap: 10px; align-items: center; margin-bottom: 15px;">
                        <input type="text" 
                               id="client-id-input" 
                               value="${Utils.escapeHtml(clientId)}" 
                               readonly 
                               style="flex: 1; padding: 10px; border: 1px solid #ddd; border-radius: 4px; font-family: monospace; font-size: 13px; background: #f8f8f8;">
                        <button type="button" 
                                id="copy-client-btn" 
                                class="btn btn-primary" 
                                style="white-space: nowrap;">
                            Copy
                        </button>
                    </div>
                    <p style="margin: 10px 0 5px 0; font-weight: 500;">Lease ID:</p>
                    <div style="display: flex; gap: 10px; align-items: center;">
                        <input type="text" 
                               id="lease-id-input" 
                               value="${Utils.escapeHtml(leaseId)}" 
                               readonly 
                               style="flex: 1; padding: 10px; border: 1px solid #ddd; border-radius: 4px; font-family: monospace; font-size: 13px; background: #f8f8f8;">
                        <button type="button" 
                                id="copy-lease-btn" 
                                class="btn btn-primary" 
                                style="white-space: nowrap;">
                            Copy
                        </button>
                    </div>
                </div>
                <div class="modal-actions">
                    <button type="button" class="btn btn-secondary modal-close">Close</button>
                </div>
            </div>
        `;

        document.body.appendChild(modal);
        modal.style.display = 'flex';

        const closeButtons = modal.querySelectorAll('.modal-close');
        const copyClientBtn = modal.querySelector('#copy-client-btn');
        const copyLeaseBtn = modal.querySelector('#copy-lease-btn');
        const clientIdInput = modal.querySelector('#client-id-input');
        const leaseIdInput = modal.querySelector('#lease-id-input');

        const closeModal = () => {
            if (modal.parentNode) {
                modal.parentNode.removeChild(modal);
            }
        };

        const setupCopyButton = (btn, input) => {
            if (btn && input) {
                btn.addEventListener('click', () => {
                    input.select();
                    document.execCommand('copy');
                    const originalText = btn.textContent;
                    btn.textContent = 'Copied!';
                    btn.style.background = '#28a745';
                    setTimeout(() => {
                        btn.textContent = originalText;
                        btn.style.background = '';
                    }, 2000);
                });
            }
        };

        setupCopyButton(copyClientBtn, clientIdInput);
        setupCopyButton(copyLeaseBtn, leaseIdInput);

        closeButtons.forEach(btn => btn.addEventListener('click', closeModal));
        modal.addEventListener('click', (e) => {
            if (e.target === modal) {
                closeModal();
            }
        });
    }

    formatParentInfo(lock) {
        if (lock.parent && lock.parent !== '') {
            return `<div class="lock-parent-info" title="Child of ${Utils.escapeHtml(lock.parent)}">↳ ${Utils.escapeHtml(lock.parent)}</div>`;
        }
        return '';
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
