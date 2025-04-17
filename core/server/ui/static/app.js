document.addEventListener('DOMContentLoaded', () => {
    const tokenValue = document.getElementById('token-value');
    const tokenExpires = document.getElementById('token-expires');
    const tokenEmail = document.getElementById('token-email');
    const copyButton = document.getElementById('copy-token');

    // Generate token automatically when the page loads
    generateToken();

    // Setup copy button
    copyButton.addEventListener('click', () => {
        copyToClipboard(tokenValue.textContent);
    });

    async function generateToken() {
        try {
            // Make API call to server to generate token
            const response = await fetch('/api/token', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                }
            });
            
            if (!response.ok) {
                throw new Error(`Server error: ${response.status}`);
            }
            
            const data = await response.json();
            const token = data.token;
            const expires = data.expires;
            
            // Display the token and its information
            tokenValue.textContent = token;
            
            // Get email from the JWT payload
            const [, payload] = token.split('.');
            if (payload) {
                try {
                    const decodedData = JSON.parse(atob(payload));
                    tokenEmail.textContent = decodedData.email || 'Not available';
                } catch (e) {
                    tokenEmail.textContent = 'Error decoding token';
                }
            }
            
            // Format expiration date
            if (expires) {
                const expiresDate = new Date(expires);
                tokenExpires.textContent = expiresDate.toLocaleString();
            } else {
                tokenExpires.textContent = 'Not specified';
            }
            
            // Auto-select the token for easy copying
            setTimeout(() => {
                selectText(tokenValue);
            }, 500);
        } catch (error) {
            console.error('Error generating token:', error);
            tokenValue.textContent = 'Error generating token. Please refresh the page.';
            tokenValue.style.color = 'red';
        }
    }

    function copyToClipboard(text) {
        navigator.clipboard.writeText(text)
            .then(() => {
                // Visual feedback for copy
                const originalText = copyButton.textContent;
                copyButton.textContent = 'Copied!';
                copyButton.style.backgroundColor = '#27ae60';
                
                setTimeout(() => {
                    copyButton.textContent = originalText;
                    copyButton.style.backgroundColor = '';
                }, 2000);
            })
            .catch(err => {
                console.error('Failed to copy text: ', err);
                alert('Failed to copy to clipboard. Please select and copy manually.');
            });
    }

    function selectText(element) {
        const range = document.createRange();
        range.selectNodeContents(element);
        const selection = window.getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
    }
});