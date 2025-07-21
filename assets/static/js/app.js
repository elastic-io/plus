class RepoManager {
    constructor() {
        this.init();
    }

    init() {
        this.bindEvents();
        this.loadRepositories();
    }

    bindEvents() {
        const uploadForm = document.getElementById('uploadForm');
        if (uploadForm) {
            uploadForm.addEventListener('submit', this.handleUploadSubmit.bind(this));
        }

        const createRepoForm = document.getElementById('createRepoForm');
        if (createRepoForm) {
            createRepoForm.addEventListener('submit', this.handleCreateRepoSubmit.bind(this));
        }

        const refreshReposBtn = document.getElementById('refreshRepos');
        if (refreshReposBtn) {
            refreshReposBtn.addEventListener('click', this.loadRepositories.bind(this));
        }
    }

    handleUploadSubmit(e) {
        e.preventDefault();
        this.uploadPackage();
    }

    handleCreateRepoSubmit(e) {
        e.preventDefault();
        this.createRepository();
    }

    // 统一的状态解析方法
    parseResponseStatus(data) {
        // 处理嵌套的 Status 结构：data.Status.status 或 data.status
        const status = data.Status || data.status;

        if (typeof status === 'string') {
            return {
                status: status,
                message: data.message || '',
                server: data.server || ''
            };
        } else if (typeof status === 'object' && status !== null) {
            return {
                status: status.status || 'unknown',
                message: status.message || data.message || '',
                server: status.server || data.server || ''
            };
        } else {
            return {
                status: 'unknown',
                message: data.message || 'Unknown response format',
                server: ''
            };
        }
    }

    async loadRepositories() {
        try {
            console.log('Loading repositories...');
            const response = await fetch('/repos');

            console.log('Response status:', response.status);

            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }

            const contentType = response.headers.get('content-type');
            if (!contentType || !contentType.includes('application/json')) {
                const text = await response.text();
                console.error('Non-JSON response:', text);
                throw new Error('Server returned non-JSON response');
            }

            const data = await response.json();
            console.log('Repository data received:', data);
            
            // 使用统一的状态解析方法
            const statusInfo = this.parseResponseStatus(data);

            if (statusInfo.status === 'success') {
                console.log('Repositories loaded successfully:', data.repositories);
                this.updateRepositorySelect(data.repositories || []);
                this.updateRepositoryList(data.repositories || [], data.tree || {});
            } else {
                console.error('Server returned error:', data);
                throw new Error(statusInfo.message || 'Unknown server error');
            }
        } catch (error) {
            console.error('Failed to load repositories:', error);
            this.showResult('repoList', 'Failed to load repositories: ' + error.message, 'error');

            // 显示错误状态
            const listContainer = document.getElementById('repoList');
            if (listContainer) {
                listContainer.innerHTML = `
                    <div class="error-state">
                        <p>❌ Failed to load repositories</p>
                        <p class="error-details">${error.message}</p>
                        <button onclick="repoManager.loadRepositories()" class="btn-secondary">
                            Retry
                        </button>
                    </div>
                `;
            }
        }
    }

    updateRepositorySelect(repositories) {
        const select = document.getElementById('repository');
        if (!select) return;
        
        select.innerHTML = '<option value="">Select Repository</option>';

        repositories.sort((a, b) => {
            const depthA = (a.match(/\//g) || []).length;
            const depthB = (b.match(/\//g) || []).length;
            if (depthA !== depthB) {
                return depthA - depthB;
            }
            return a.localeCompare(b);
        });
        
        repositories.forEach(repo => {
            const option = document.createElement('option');
            option.value = repo;
            const depth = (repo.match(/\//g) || []).length;
            const indent = '  '.repeat(depth);
            option.textContent = indent + repo;
            select.appendChild(option);
        });
    }

    updateRepositoryList(repositories, tree) {
        const listContainer = document.getElementById('repoList');
        if (!listContainer) return;
        
        listContainer.innerHTML = '';

        if (repositories.length === 0) {
            listContainer.innerHTML = '<p>No repositories found.</p>';
            return;
        }

        if (tree && Object.keys(tree).length > 0) {
            const treeHtml = this.renderRepoTree(tree);
            listContainer.innerHTML = treeHtml;
        } else {
            repositories.forEach(repo => {
                const repoItem = document.createElement('div');
                repoItem.className = 'repo-item';
                repoItem.innerHTML = `
                    <div class="repo-name">📦 ${repo}</div>
                    <div class="repo-actions">
                        <button class="btn-refresh" onclick="repoManager.refreshRepository('${repo}')">
                            Refresh Metadata
                        </button>
                        <button class="btn-info" onclick="repoManager.showRepositoryInfo('${repo}')">
                            Info
                        </button>
                    </div>
                `;
                listContainer.appendChild(repoItem);
            });
        }
    }

    renderRepoTree(tree, level = 0) {
        let html = '';
        
        for (const [name, node] of Object.entries(tree)) {
            if (node.type === 'repo') {
                html += `
                    <div class="repo-item" style="margin-left: ${level * 20}px;">
                        <div class="repo-name">📦 ${name} <span class="repo-path">(${node.path})</span></div>
                        <div class="repo-actions">
                            <button class="btn-refresh" onclick="repoManager.refreshRepository('${node.path}')">
                                Refresh Metadata
                            </button>
                            <button class="btn-info" onclick="repoManager.showRepositoryInfo('${node.path}')">
                                Info
                            </button>
                        </div>
                    </div>
                `;
            } else if (node.type === 'directory' && node.children) {
                html += `
                    <div class="repo-directory" style="margin-left: ${level * 20}px;">
                        <div class="directory-name">📁 ${name}/</div>
                        ${this.renderRepoTree(node.children, level + 1)}
                    </div>
                `;
            }
        }
        
        return html;
    }

    async uploadPackage() {
        console.log('uploadPackage method called');
        
        const form = document.getElementById('uploadForm');
        if (!form) {
            console.error('Upload form not found');
            return;
        }

        const formData = new FormData(form);
        const repository = formData.get('repository');
        const file = formData.get('file');

        console.log('Upload attempt:', { repository, file: file?.name });

        if (!repository || !file) {
            this.showResult('uploadResult', 'Please select repository and file', 'error');
            return;
        }

        try {
            const uploadUrl = `/repo/${encodeURIComponent(repository)}/upload`;
            console.log('Upload URL:', uploadUrl);

            const response = await fetch(uploadUrl, {
                method: 'POST',
                body: formData
            });

            console.log('Upload response status:', response.status);

            const data = await response.json();
            console.log('Upload response data:', data);
            
            // 使用统一的状态解析方法
            const statusInfo = this.parseResponseStatus(data);

            if (response.ok && statusInfo.status === 'success') {
                this.showResult('uploadResult', statusInfo.message || 'Upload successful', 'success');
                form.reset();
                this.resetFileUploadArea();
            } else {
                this.showResult('uploadResult', statusInfo.message || 'Upload failed', 'error');
            }
        } catch (error) {
            console.error('Upload error:', error);
            this.showResult('uploadResult', 'Upload failed: ' + error.message, 'error');
        }
    }

    resetFileUploadArea() {
        const fileUploadArea = document.getElementById('fileUploadArea');
        const fileUploadContent = document.getElementById('fileUploadContent');
        const fileInfo = document.getElementById('fileInfo');

        if (fileUploadArea) {
            fileUploadArea.classList.remove('file-selected');
        }
        
        if (fileInfo) {
            fileInfo.style.display = 'none';
        }
        
        if (fileUploadContent) {
            const svg = fileUploadContent.querySelector('svg');
            const p = fileUploadContent.querySelector('p');
            if (svg) svg.style.display = 'block';
            if (p) p.style.display = 'block';
        }
    }

    async createRepository() {
        console.log('createRepository method called');
        
        const form = document.getElementById('createRepoForm');
        if (!form) {
            console.error('Create repo form not found');
            return;
        }

        const formData = new FormData(form);
        
        const data = {
            name: formData.get('name'),
            path: formData.get('path') || '',
            description: formData.get('description') || ''
        };

        console.log('Create repository data:', data);

        try {
            const response = await fetch('/repos', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(data)
            });

            const result = await response.json();
            console.log('Create repository response:', result);
            
            // 使用统一的状态解析方法
            const statusInfo = this.parseResponseStatus(result);

            if (response.ok && statusInfo.status === 'success') {
                this.showResult('createResult', statusInfo.message || 'Repository created successfully', 'success');
                form.reset();
                this.loadRepositories();
            } else {
                this.showResult('createResult', statusInfo.message || 'Creation failed', 'error');
            }
        } catch (error) {
            console.error('Create repository error:', error);
            this.showResult('createResult', 'Creation failed: ' + error.message, 'error');
        }
    }

    async refreshRepository(repoName) {
        try {
            console.log('Refreshing repository:', repoName);
            
            const refreshUrl = `/repo/${encodeURIComponent(repoName)}/refresh`;
            console.log('Refresh URL:', refreshUrl);

            const response = await fetch(refreshUrl, {
                method: 'POST'
            });

            console.log('Refresh response status:', response.status);

            const contentType = response.headers.get('content-type');
            if (!contentType || !contentType.includes('application/json')) {
                throw new Error(`Server returned non-JSON response: ${response.status} ${response.statusText}`);
            }

            const data = await response.json();
            console.log('Refresh response data:', data);
            
            // 使用统一的状态解析方法
            const statusInfo = this.parseResponseStatus(data);
            const repo = data.repo || repoName;

            if (response.ok && statusInfo.status === 'success') {
                alert(`Repository ${repo} metadata refreshed successfully`);
            } else {
                alert(`Failed to refresh ${repoName}: ${statusInfo.message || 'Unknown error'}`);
            }
        } catch (error) {
            console.error('Refresh error:', error);
            alert(`Failed to refresh ${repoName}: ${error.message}`);
        }
    }

    async showRepositoryInfo(repoName) {
        try {
            console.log('Getting repository info:', repoName);

            const infoUrl = `/repo/${encodeURIComponent(repoName)}`;
            console.log('Info URL:', infoUrl);

            const response = await fetch(infoUrl);
            const data = await response.json();

            console.log('Repository info response:', data);

            // 使用统一的状态解析方法
            const statusInfo = this.parseResponseStatus(data);

            if (response.ok && statusInfo.status === 'success') {
                // 处理可能的不同响应结构
                const repoInfo = {
                    name: data.name || repoName,
                    package_count: data.package_count || data.PackageCount || 0,
                    rpm_count: data.rpm_count || data.RPMCount || 0,
                    deb_count: data.deb_count || data.DEBCount || 0,
                    total_size: data.total_size || data.TotalSize || 0,
                    packages: data.packages || data.Packages || []
                };

                const info = `
Repository: ${repoInfo.name}
Packages: ${repoInfo.package_count}
RPM Packages: ${repoInfo.rpm_count}
DEB Packages: ${repoInfo.deb_count}
Total Size: ${this.formatFileSize(repoInfo.total_size)}

Package List:
${repoInfo.packages.map(pkg => {
                    const pkgName = pkg.name || pkg.Name || 'Unknown';
                    const pkgSize = pkg.size || pkg.Size || 0;
                    return `- ${pkgName} (${this.formatFileSize(pkgSize)})`;
                }).join('\n')}
                `;
                alert(info);
            } else {
                alert(`Failed to get info for ${repoName}: ${statusInfo.message || 'Unknown error'}`);
            }
        } catch (error) {
            console.error('Info error:', error);
            alert(`Failed to get info for ${repoName}: ${error.message}`);
        }
    }

    formatFileSize(bytes) {
        if (bytes === 0) return '0 Bytes';
        const k = 1024;
        const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    showResult(elementId, message, type) {
        const element = document.getElementById(elementId);
        if (element) {
            element.textContent = message;
            element.className = `result ${type}`;
            element.style.display = 'block';
            
            setTimeout(() => {
                element.style.display = 'none';
            }, 3000);
        } else {
            console.error(`Element with id '${elementId}' not found`);
        }
    }
}

// 确保在 DOM 加载完成后初始化
let repoManager;

document.addEventListener('DOMContentLoaded', function() {
    console.log('DOM loaded, initializing RepoManager');

    repoManager = new RepoManager();
    initFileUpload();
});

// 文件上传初始化函数
function initFileUpload() {
    const fileUploadArea = document.getElementById('fileUploadArea');
    const fileInput = document.getElementById('file');
    const fileUploadContent = document.getElementById('fileUploadContent');
    const fileInfo = document.getElementById('fileInfo');
    const fileName = document.getElementById('fileName');
    const fileSize = document.getElementById('fileSize');
    const browseLink = document.querySelector('.browse-link');

    if (!fileUploadArea || !fileInput) {
        console.error('File upload elements not found');
        return;
    }

    console.log('File upload elements found, binding events');

    fileUploadArea.addEventListener('click', function() {
        console.log('File upload area clicked');
        fileInput.click();
    });

    if (browseLink) {
        browseLink.addEventListener('click', function(e) {
            console.log('Browse link clicked');
            e.stopPropagation();
            fileInput.click();
        });
    }

    fileInput.addEventListener('change', function(e) {
        console.log('File input changed:', e.target.files);
        handleFileSelect(e.target.files[0]);
    });

    fileUploadArea.addEventListener('dragover', function(e) {
        e.preventDefault();
        e.stopPropagation();
        fileUploadArea.classList.add('drag-over');
    });

    fileUploadArea.addEventListener('dragleave', function(e) {
        e.preventDefault();
        e.stopPropagation();
        fileUploadArea.classList.remove('drag-over');
    });

    fileUploadArea.addEventListener('drop', function(e) {
        e.preventDefault();
        e.stopPropagation();
        fileUploadArea.classList.remove('drag-over');
        
        const files = e.dataTransfer.files;
        if (files.length > 0) {
            const file = files[0];
            if (file.name.endsWith('.rpm') || file.name.endsWith('.deb')) {
                handleFileSelect(file);
                const dt = new DataTransfer();
                dt.items.add(file);
                fileInput.files = dt.files;
            } else {
                showMessage('Please select a valid RPM or DEB file.', 'error');
            }
        }
    });

    function handleFileSelect(file) {
        console.log('Handling file select:', file);
        if (file && fileName && fileSize && fileInfo) {
            fileName.textContent = file.name;
            fileSize.textContent = formatFileSize(file.size);
            fileInfo.style.display = 'block';
            fileUploadArea.classList.add('file-selected');
            
            const svg = fileUploadContent.querySelector('svg');
            const p = fileUploadContent.querySelector('p');
            if (svg) svg.style.display = 'none';
            if (p) p.style.display = 'none';
        }
    }

    function formatFileSize(bytes) {
        if (bytes === 0) return '0 Bytes';
        const k = 1024;
        const sizes = ['Bytes', 'KB', 'MB', 'GB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    function showMessage(message, type) {
        const uploadResult = document.getElementById('uploadResult');
        if (uploadResult) {
            uploadResult.innerHTML = `<div class="message ${type}">${message}</div>`;
            setTimeout(() => {
                uploadResult.innerHTML = '';
            }, 3000);
        }
    }
}