/**
 * Main application logic for the Glock Dashboard
 */

class GlockDashboard {
    constructor() {
        this.initialized = false;
        this.init();
    }

    init() {
        if (this.initialized) return;

        console.log('Initializing Glock Dashboard...');

        // Wait for DOM to be fully loaded
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', () => this.setup());
        } else {
            this.setup();
        }

        this.initialized = true;
    }

    setup() {
        this.bindGlobalEvents();
        this.setupKeyboardShortcuts();
        this.showWelcomeMessage();

        // Auto-load locks if lock manager isn't initialized yet
        if (!window.lockManager) {
            setTimeout(() => {
                if (window.location.pathname === '/' && !window.lockManager) {
                    console.warn('LockManager not found, initializing fallback...');
                    // This will be handled by locks.js
                }
            }, 100);
        }
    }

    bindGlobalEvents() {
        // Handle unexpected errors
        window.addEventListener('error', (e) => {
            console.error('Global error:', e.error);
            this.showError('An unexpected error occurred. Please refresh the page.');
        });

        // Handle unhandled promise rejections
        window.addEventListener('unhandledrejection', (e) => {
            console.error('Unhandled promise rejection:', e.reason);
            this.showError('An unexpected error occurred. Please refresh the page.');
        });

        // Handle online/offline status
        window.addEventListener('online', () => {
            this.showStatus('Connection restored', 'success');
            if (window.lockManager) {
                window.lockManager.loadLocks();
            }
        });

        window.addEventListener('offline', () => {
            this.showStatus('Connection lost', 'warning');
        });
    }

    setupKeyboardShortcuts() {
        document.addEventListener('keydown', (e) => {
            // Ctrl/Cmd + R to refresh
            if ((e.ctrlKey || e.metaKey) && e.key === 'r') {
                e.preventDefault();
                if (window.lockManager) {
                    window.lockManager.loadLocks();
                }
            }

            // Ctrl/Cmd + N to create new lock
            if ((e.ctrlKey || e.metaKey) && e.key === 'n') {
                e.preventDefault();
                if (window.lockManager) {
                    window.lockManager.showCreateModal();
                }
            }

            // Escape to close modals
            if (e.key === 'Escape') {
                if (window.lockManager) {
                    window.lockManager.closeAllModals();
                }
            }

            // Ctrl/Cmd + F to focus search
            if ((e.ctrlKey || e.metaKey) && e.key === 'f') {
                e.preventDefault();
                const searchInput = document.getElementById('search-input');
                if (searchInput) {
                    searchInput.focus();
                    searchInput.select();
                }
            }
        });
    }

    showWelcomeMessage() {
        // Small delay to ensure everything is loaded
        setTimeout(() => {
            console.log('🚀 Glock Dashboard loaded successfully!');
            console.log('💡 Keyboard shortcuts:');
            console.log('   Ctrl/Cmd + R: Refresh locks');
            console.log('   Ctrl/Cmd + N: Create new lock');
            console.log('   Ctrl/Cmd + F: Focus search');
            console.log('   Escape: Close modals');
        }, 1000);
    }

    showError(message) {
        if (typeof Utils !== 'undefined' && Utils.showAlert) {
            Utils.showAlert('Error', message, 'error');
        } else {
            alert(message);
        }
    }

    showStatus(message, type = 'info') {
        // Create a temporary status message
        const statusEl = document.createElement('div');
        statusEl.className = `status-message status-${type}`;
        statusEl.textContent = message;
        statusEl.style.cssText = `
            position: fixed;
            top: 20px;
            right: 20px;
            padding: 12px 20px;
            border-radius: 6px;
            color: white;
            font-weight: 500;
            z-index: 10000;
            animation: slideInRight 0.3s ease;
            box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
        `;

        // Set background color based on type
        const colors = {
            success: '#28a745',
            error: '#dc3545',
            warning: '#ffc107',
            info: '#17a2b8'
        };
        statusEl.style.backgroundColor = colors[type] || colors.info;

        document.body.appendChild(statusEl);

        // Remove after 3 seconds
        setTimeout(() => {
            statusEl.style.animation = 'slideOutRight 0.3s ease';
            setTimeout(() => {
                if (statusEl.parentNode) {
                    statusEl.parentNode.removeChild(statusEl);
                }
            }, 300);
        }, 3000);
    }
}

// Add CSS for status messages and animations
const statusStyles = `
    @keyframes slideInRight {
        from {
            transform: translateX(100%);
            opacity: 0;
        }
        to {
            transform: translateX(0);
            opacity: 1;
        }
    }

    @keyframes slideOutRight {
        from {
            transform: translateX(0);
            opacity: 1;
        }
        to {
            transform: translateX(100%);
            opacity: 0;
        }
    }

    .status-message {
        font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
        font-size: 14px;
    }
`;

// Inject styles
const styleSheet = document.createElement('style');
styleSheet.textContent = statusStyles;
document.head.appendChild(styleSheet);

// Initialize the dashboard
window.glockDashboard = new GlockDashboard();

// Export for debugging
if (typeof module !== 'undefined' && module.exports) {
    module.exports = GlockDashboard;
}
