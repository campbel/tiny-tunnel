// Create a custom pushstate event dispatcher
const dispatchPushState = (state, title, url) => {
  const pushStateEvent = new CustomEvent('pushstate', {
    detail: { state, title, url }
  });
  window.dispatchEvent(pushStateEvent);
};

// Override the native pushState to dispatch our custom event
const originalPushState = history.pushState;
history.pushState = function(state, title, url) {
  originalPushState.apply(this, [state, title, url]);
  dispatchPushState(state, title, url);
};

// Define the custom router-link element
class RouterLink extends HTMLElement {
  constructor() {
    super();
    this._href = '';
    this._id = '';
    this._text = '';
  }

  static get observedAttributes() {
    return ['href', 'id', 'text'];
  }

  connectedCallback() {
    this._id = document.querySelector('#app-container')?.dataset.id || '';
    this._href = this.getAttribute('href') || '';
    this._text = this.getAttribute('text') || '';
    this._render();
    this._addEventListeners();
    this._checkActive();
  }

  attributeChangedCallback(name, oldValue, newValue) {
    if (oldValue !== newValue) {
      this[`_${name}`] = newValue;
      this._render();
    }
  }

  _render() {
    // Create the internal link structure
    this.innerHTML = `
      <a href="${this._href}">
        ${this._text}
      </a>
    `;
  }

  _addEventListeners() {
    const link = this.querySelector('a');
    if (link) {
      link.addEventListener('click', (event) => {
        event.preventDefault();
        this._navigate();
      });
    }

    window.addEventListener('popstate', () => this._checkActive());
    window.addEventListener('pushstate', () => this._checkActive());
  }

  _navigate() {
    const path = this._href;
    history.pushState({ path }, '', path);

    // Make the fetch request with the ID
    const url = new URL(path, window.location.origin);
    url.searchParams.append('id', this._id);
    fetch(url.toString());

    // Update active states
    this._checkActive();
  }

  _checkActive() {
    // Check if this link's path matches current location
    const currentPath = window.location.pathname;
    if (currentPath === this._href) {
      this.setAttribute('active', '');
    } else {
      this.removeAttribute('active');
    }
  }
}

// Register the custom element
customElements.define('router-link', RouterLink);

