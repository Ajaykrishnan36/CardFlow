// CardFlow Frontend API Client
// Automatically connects to local backend (http://127.0.0.1:8080) or live Render backend

const getBaseUrl = () => {
  if (typeof window !== 'undefined') {
    // Running inside the native Capacitor shell (Android/iOS) — the bundled
    // app has no local backend to reach, so always use the live one.
    if (window.Capacitor?.isNativePlatform?.()) {
      return 'https://cardflow-api-fsij.onrender.com/api/v1';
    }
    // When testing on localhost or 127.0.0.1, connect to local backend at 8080
    if (window.location.hostname === 'localhost' || window.location.hostname === '127.0.0.1') {
      return 'http://127.0.0.1:8080/api/v1';
    }
    // When running on Render web service
    if (window.location.origin.includes('onrender.com')) {
      return `${window.location.origin}/api/v1`;
    }
  }
  return 'https://cardflow-api-fsij.onrender.com/api/v1';
};

export const API_BASE_URL = getBaseUrl();

/** Build full URL for API-relative paths like /api/v1/cards/{id}/original-image */
export function resolveApiUrl(path) {
  if (!path) return '';
  if (path.startsWith('data:') || path.startsWith('blob:') || path.startsWith('http')) return path;
  const origin = API_BASE_URL.replace(/\/api\/v1\/?$/, '');
  return path.startsWith('/') ? `${origin}${path}` : `${API_BASE_URL}/${path}`;
}

/** Fetch authenticated original card image and return a blob URL for <img src> */
export async function fetchCardOriginalImageUrl(imagePathOrCardId, token) {
  if (!token) return null;
  let path = imagePathOrCardId;
  if (path && !path.includes('/') && !path.startsWith('data:')) {
    path = `/api/v1/cards/${path}/original-image`;
  }
  if (!path) return null;
  if (path.startsWith('data:') || path.startsWith('blob:')) return path;
  const url = resolveApiUrl(path);
  try {
    const res = await fetch(url, { headers: { Authorization: `Bearer ${token}` } });
    if (!res.ok) return null;
    const blob = await res.blob();
    return URL.createObjectURL(blob);
  } catch (e) {
    console.warn('Could not load original card image', e);
    return null;
  }
}

export function cardOriginalImagePath(cardId, side = 'front') {
  if (!cardId) return '';
  const qs = side && side !== 'front' ? `?side=${side}` : '';
  return `/api/v1/cards/${cardId}/original-image${qs}`;
}

/** Fetch a shared card's original image with no auth — used by the public share-link view */
export async function fetchPublicCardImageUrl(cardId, side = 'front') {
  if (!cardId) return null;
  const qs = side && side !== 'front' ? `?side=${side}` : '';
  const url = resolveApiUrl(`/api/v1/public/cards/${cardId}/original-image${qs}`);
  try {
    const res = await fetch(url);
    if (!res.ok) return null;
    const blob = await res.blob();
    return URL.createObjectURL(blob);
  } catch (e) {
    console.warn('Could not load shared card image', e);
    return null;
  }
}

// Normalizes 10-digit or raw numbers to E.164 (+91...)
const formatE164 = (raw) => {
  if (!raw) return '';
  const digits = raw.replace(/\D/g, '');
  if (digits.length === 10) return `+91${digits}`;
  if (digits.length === 12 && digits.startsWith('91')) return `+${digits}`;
  if (raw.startsWith('+')) return raw;
  return `+91${digits}`;
};

export const apiClient = {
  // 1. Auth: Send OTP
  async sendOtp(phone) {
    const formattedPhone = formatE164(phone);
    console.log('📡 [API CALL] POST /auth/otp/send', { phone: formattedPhone });
    try {
      const res = await fetch(`${API_BASE_URL}/auth/otp/send`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ phone: formattedPhone, platform: 'web', device_id: 'browser-client' })
      });
      const data = await res.json();
      console.log('📥 [API RESPONSE] /auth/otp/send', data);
      if (!res.ok) {
        return {
          status: 'error',
          error: { message: data?.error?.message || "Couldn't send OTP. Please try again." }
        };
      }
      return data;
    } catch (e) {
      console.warn('API /auth/otp/send failed, falling back:', e);
      return { status: 'error', error: { message: "Couldn't send OTP. Please try again." } };
    }
  },

  // 2. Auth: Verify OTP
  async verifyOtp(phone, otp) {
    const formattedPhone = formatE164(phone);
    console.log('📡 [API CALL] POST /auth/otp/verify', { phone: formattedPhone, otp });
    try {
      const res = await fetch(`${API_BASE_URL}/auth/otp/verify`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ phone: formattedPhone, otp, otp_code: otp, platform: 'web', device_id: 'browser-client' })
      });
      const data = await res.json();
      console.log('📥 [API RESPONSE] /auth/otp/verify', data);
      if (!res.ok) {
        return {
          status: 'error',
          error: { message: data?.error?.message || data?.message || 'Invalid OTP. Please check the code and try again.' }
        };
      }
      return data;
    } catch (e) {
      console.warn('API /auth/otp/verify failed, falling back:', e);
      return { status: 'error', error: { message: 'Something went wrong. Please try again.' } };
    }
  },

  async getMe(token = '') {
    const headers = {};
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/users/me`, { headers });
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message || 'Failed to load profile');
    return data.data || data;
  },

  async updateProfile(payload, token = '') {
    const headers = { 'Content-Type': 'application/json' };
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/users/me`, {
      method: 'PATCH',
      headers,
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message || 'Profile update failed');
    return data.data || data;
  },

  // Updates the caller's own mobile number after OTP verification.
  async changePhone(phone, otpCode, token = '') {
    const formattedPhone = formatE164(phone);
    const headers = { 'Content-Type': 'application/json' };
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/users/me/phone`, {
      method: 'PATCH',
      headers,
      body: JSON.stringify({ phone: formattedPhone, otp_code: otpCode })
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message || 'Could not update mobile number');
    return data.data || data;
  },

  // CardFlow Premium (RevenueCat). The server's stored state is what premium
  // APIs enforce; the app shows it and asks the server to re-sync after a purchase.
  async getBillingStatus(token = '') {
    const headers = {};
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/billing/status`, { headers });
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message || 'Could not load subscription status');
    return data.data || data;
  },

  // Asks the server to re-read this user's entitlements from RevenueCat.
  async syncBilling(token = '') {
    const headers = {};
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/billing/sync`, { method: 'POST', headers });
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message || 'Could not refresh subscription');
    return data.data || data;
  },

  async getBillingTransactions(token = '') {
    const headers = {};
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/billing/transactions`, { headers });
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message || 'Could not load transactions');
    return data.data || data;
  },

  // Uploads a full snapshot of the phone's contacts to the cloud backup.
  async backupContacts(contacts, token = '') {
    const headers = { 'Content-Type': 'application/json' };
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/contacts/backup`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ contacts })
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message || 'Could not back up contacts');
    return data.data || data;
  },

  // Reports whether a cloud backup exists yet, without returning the data.
  async getContactBackupStatus(token = '') {
    const headers = {};
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/contacts/backup/status`, { headers });
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message || 'Could not check backup status');
    return data.data || data;
  },

  // Fetches the actual backed-up contacts, for restoring onto a phone.
  async getContactBackup(token = '') {
    const headers = {};
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/contacts/backup`, { headers });
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message || 'Could not load contact backup');
    return data.data || data;
  },

  // 3. Discovery: Categories
  async getCategories() {
    console.log('📡 [API CALL] GET /categories');
    try {
      const res = await fetch(`${API_BASE_URL}/categories`);
      const data = await res.json();
      return data.data?.categories || data.data || [];
    } catch (e) {
      console.warn('API /categories failed:', e);
      return [];
    }
  },

  // 4. Discovery: Search Businesses
  async searchBusinesses(params = {}) {
    console.log('📡 [API CALL] GET /businesses/search', params);
    try {
      const url = new URL(`${API_BASE_URL}/businesses/search`);
      Object.keys(params).forEach(k => {
        if (params[k] != null && params[k] !== '') url.searchParams.append(k, params[k]);
      });
      const res = await fetch(url.toString());
      const data = await res.json();
      return data.data?.businesses || data.data || [];
    } catch (e) {
      console.warn('API /businesses/search failed:', e);
      return [];
    }
  },

  // 5. Card OCR Scanner & Extraction (Guaranteed Auto-Fill)
  async scanCard(imageKey = 'lipi-traders-card.jpg', token = '') {
    console.log('📡 [API CALL] POST /cards/scan', { image_object_key: imageKey });
    try {
      const headers = { 'Content-Type': 'application/json' };
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/cards/scan`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ image_object_key: imageKey })
      });
      const data = await res.json();
      console.log('📥 [API RESPONSE] /cards/scan', data);
      
      const payload = data.data || data;
      if (payload && (payload.company || payload.person_name || payload.phones)) {
        return payload;
      }
    } catch (e) {
      console.warn('API /cards/scan network error, using on-device OCR:', e);
    }
    return null;
  },

  // 6. Save Card to Vault
  async saveCard(cardData, token = '') {
    console.log('📡 [API CALL] POST /cards', { ...cardData, original_card_image_url: cardData.original_card_image_url ? '[image]' : '' });
    try {
      const headers = { 'Content-Type': 'application/json' };
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/cards`, {
        method: 'POST',
        headers,
        body: JSON.stringify(cardData)
      });
      const data = await res.json();
      console.log('📥 [API RESPONSE] /cards', data);
      if (!res.ok) {
        throw new Error(data?.error?.message || 'Could not save card');
      }
      const saved = data.data || data;
      if (!saved?.id) {
        throw new Error('Save succeeded without a card ID — please try again.');
      }
      return saved;
    } catch (e) {
      console.warn('API /cards failed:', e);
      throw e;
    }
  },

  async uploadCardOriginalImage(cardId, imageData, token = '', side = 'front') {
    console.log('📡 [API CALL] POST /cards/{id}/original-image', cardId, side);
    try {
      const headers = { 'Content-Type': 'application/json' };
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/cards/${cardId}/original-image`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ image_data: imageData, side: side || 'front' })
      });
      const raw = await res.text();
      let data = {};
      try {
        data = raw ? JSON.parse(raw) : {};
      } catch (_) {
        throw new Error(
          res.ok
            ? 'Invalid response from image upload'
            : `Image upload failed (${res.status}). Restart the local API if this persists.`
        );
      }
      if (!res.ok) {
        throw new Error(data?.error?.message || data?.message || `Image upload failed (${res.status})`);
      }
      return data.data || data;
    } catch (e) {
      console.warn('API upload original image failed:', e);
      throw e;
    }
  },

  async updateCard(cardId, cardData, token = '') {
    const headers = { 'Content-Type': 'application/json' };
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/cards/${cardId}`, {
      method: 'PATCH',
      headers,
      body: JSON.stringify(cardData)
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message || 'Could not update card');
    return data.data || data;
  },

  // Removes a saved card from the vault (soft delete) — used both for
  // deleting a scanned card and for "unsaving" a business.
  async deleteCard(cardId, token = '') {
    const headers = {};
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/cards/${cardId}`, { method: 'DELETE', headers });
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message || 'Could not remove card');
    return data.data || data;
  },

  // Loads a shared card's public fields — no auth, used by the "/share/{id}" link view.
  async getPublicCard(cardId) {
    const res = await fetch(`${API_BASE_URL}/public/cards/${cardId}`);
    const data = await res.json();
    if (!res.ok) throw new Error(data?.error?.message || 'This shared card link is no longer available.');
    return data.data || data;
  },

  // 7. Get Saved Cards in Vault
  async getCards(token = '') {
    console.log('📡 [API CALL] GET /cards');
    const headers = {};
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/cards`, { headers });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data?.error?.message || 'Could not load saved cards');
    }
    const list = data.data?.cards || data.cards;
    return Array.isArray(list) ? list : [];
  },

  // 8. Admin: Dashboard Stats
  async getAdminDashboard(token = '') {
    console.log('📡 [API CALL] GET /admin/dashboard');
    try {
      const headers = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/admin/dashboard`, { headers });
      const data = await res.json();
      return data.data || data;
    } catch (e) {
      console.warn('API /admin/dashboard failed:', e);
      return null;
    }
  },

  // 9. Admin: List Users
  async getAdminUsers(token = '') {
    console.log('📡 [API CALL] GET /admin/users');
    try {
      const headers = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/admin/users`, { headers });
      const data = await res.json();
      return data.data?.users || data.data || [];
    } catch (e) {
      console.warn('API /admin/users failed:', e);
      return [];
    }
  },

  // 10. Admin: Grant Free Access
  async grantAccess(payload, token = '') {
    console.log('📡 [API CALL] POST /admin/grant-access', payload);
    try {
      const headers = { 'Content-Type': 'application/json' };
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/admin/users/grant-access`, {
        method: 'POST',
        headers,
        body: JSON.stringify(payload)
      });
      const data = await res.json();
      return data.data || data;
    } catch (e) {
      console.warn('API /admin/grant-access failed:', e);
      return null;
    }
  },

  // 11. Admin: Create Business Manually
  async createBusinessManual(payload, token = '') {
    console.log('📡 [API CALL] POST /admin/businesses/manual-create', payload);
    try {
      const headers = { 'Content-Type': 'application/json' };
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/admin/businesses/manual-create`, {
        method: 'POST',
        headers,
        body: JSON.stringify(payload)
      });
      const data = await res.json();
      return data.data || data;
    } catch (e) {
      console.warn('API /admin/businesses/manual-create failed:', e);
      return null;
    }
  },

  // 12. Admin: List Businesses
  async getAdminBusinesses(token = '') {
    console.log('📡 [API CALL] GET /admin/businesses');
    try {
      const headers = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/admin/businesses`, { headers });
      const data = await res.json();
      return data.data?.businesses || data.data || [];
    } catch (e) {
      console.warn('API /admin/businesses failed:', e);
      return [];
    }
  },

  // 13. Admin: Update Business Listing
  async updateAdminBusiness(id, payload, token = '') {
    console.log(`📡 [API CALL] PUT /admin/businesses/${id}`, payload);
    try {
      const headers = { 'Content-Type': 'application/json' };
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/admin/businesses/${id}`, {
        method: 'PUT',
        headers,
        body: JSON.stringify(payload)
      });
      const data = await res.json();
      return data.data || data;
    } catch (e) {
      console.warn(`API /admin/businesses/${id} failed:`, e);
      return null;
    }
  },

  // 14. Admin: Delete Business Listing
  async deleteAdminBusiness(id, token = '') {
    console.log(`📡 [API CALL] DELETE /admin/businesses/${id}`);
    try {
      const headers = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/admin/businesses/${id}`, {
        method: 'DELETE',
        headers
      });
      const data = await res.json();
      return data.data || data;
    } catch (e) {
      console.warn(`API /admin/businesses/${id} failed:`, e);
      return null;
    }
  },

  // 15. Admin: Delete User
  async deleteAdminUser(id, token = '') {
    console.log(`📡 [API CALL] DELETE /admin/users/${id}`);
    try {
      const headers = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/admin/users/${id}`, {
        method: 'DELETE',
        headers
      });
      const data = await res.json();
      return data.data || data;
    } catch (e) {
      console.warn(`API /admin/users/${id} failed:`, e);
      return null;
    }
  },

  // 16. Admin: Get KYC Verifications
  async getAdminVerifications(token = '') {
    console.log('📡 [API CALL] GET /admin/verification');
    try {
      const headers = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/admin/verification`, { headers });
      const data = await res.json();
      return data.data?.queue || data.data || [];
    } catch (e) {
      console.warn('API /admin/verification failed:', e);
      return [];
    }
  },

  // 17. Admin: Decide Verification
  async decideVerification(id, action, notes = '', token = '') {
    console.log(`📡 [API CALL] POST /admin/verification/${id}/decision`, { action, notes });
    try {
      const headers = { 'Content-Type': 'application/json' };
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/admin/verification/${id}/decision`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ action, notes })
      });
      const data = await res.json();
      return data.data || data;
    } catch (e) {
      console.warn(`API /admin/verification/${id}/decision failed:`, e);
      return null;
    }
  },

  // 18. Support: Create Ticket (User / Owner)
  async createSupportTicket(payload, token = '') {
    console.log('📡 [API CALL] POST /support/tickets', payload);
    try {
      const headers = { 'Content-Type': 'application/json' };
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/support/tickets`, {
        method: 'POST',
        headers,
        body: JSON.stringify(payload)
      });
      const data = await res.json();
      return data.data || data;
    } catch (e) {
      console.warn('API /support/tickets failed:', e);
      return null;
    }
  },

  // 19. Support: Get My Tickets (User / Owner)
  async getMySupportTickets(token = '') {
    console.log('📡 [API CALL] GET /support/tickets/my');
    try {
      const headers = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/support/tickets/my`, { headers });
      const data = await res.json();
      return data.data?.tickets || data.data || [];
    } catch (e) {
      console.warn('API /support/tickets/my failed:', e);
      return [];
    }
  },

  // 20. Support: Admin List All Tickets
  async getAdminSupportTickets(token = '') {
    console.log('📡 [API CALL] GET /admin/support/tickets');
    try {
      const headers = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/admin/support/tickets`, { headers });
      const data = await res.json();
      return data.data?.tickets || data.data || [];
    } catch (e) {
      console.warn('API /admin/support/tickets failed:', e);
      return [];
    }
  },

  // 21. Support: Admin Update Ticket / Reply
  async updateAdminSupportTicket(id, payload, token = '') {
    console.log(`📡 [API CALL] PATCH /admin/support/tickets/${id}`, payload);
    try {
      const headers = { 'Content-Type': 'application/json' };
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/admin/support/tickets/${id}`, {
        method: 'PATCH',
        headers,
        body: JSON.stringify(payload)
      });
      const data = await res.json();
      return data.data || data;
    } catch (e) {
      console.warn(`API /admin/support/tickets/${id} failed:`, e);
      return null;
    }
  },

  // 22. Owner: List My Businesses
  async getMyBusinesses(token = '') {
    console.log('📡 [API CALL] GET /owner/businesses');
    try {
      const headers = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/owner/businesses`, { headers });
      const data = await res.json();
      return data.data?.businesses || data.data || [];
    } catch (e) {
      console.warn('API /owner/businesses failed:', e);
      return [];
    }
  },

  // 23. Owner: Create Business
  async createMyBusiness(payload, token = '') {
    console.log('📡 [API CALL] POST /owner/businesses', payload);
    try {
      const headers = { 'Content-Type': 'application/json' };
      if (token) headers['Authorization'] = `Bearer ${token}`;
      const res = await fetch(`${API_BASE_URL}/owner/businesses`, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          name: payload.business_name || payload.name,
          description: payload.description || payload.category,
          category_id: payload.category_id || payload.category,
          address_line1: payload.address || payload.address_line1,
          locality: payload.area || payload.locality,
          city: payload.city,
          district: payload.district,
          state: payload.state,
          pincode: payload.pincode,
          phone: payload.phone,
          whatsapp: payload.whatsapp,
          email: payload.email,
          website: payload.website,
          gstin: payload.gstin,
          services: payload.services,
          front_image_data: payload.front_image_data,
          back_image_data: payload.back_image_data
        })
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data?.error?.message || 'Could not create business');
      return data.data || data;
    } catch (e) {
      console.warn('API /owner/businesses failed:', e);
      throw e;
    }
  },

  // 24. Owner: Update Business
  async updateMyBusiness(id, payload, token = '') {
    console.log('📡 [API CALL] PATCH /owner/businesses/' + id, payload);
    const headers = { 'Content-Type': 'application/json' };
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/owner/businesses/${id}`, {
      method: 'PATCH',
      headers,
      body: JSON.stringify({
        name: payload.business_name || payload.name,
        description: payload.description,
        category_id: payload.category_id || payload.category,
        address_line1: payload.address || payload.address_line1,
        locality: payload.area || payload.locality,
        city: payload.city,
        state: payload.state,
        pincode: payload.pincode,
        phone: payload.phone,
        whatsapp: payload.whatsapp,
        email: payload.email,
        website: payload.website,
        gstin: payload.gstin,
        services: payload.services,
        front_image_data: payload.front_image_data || undefined,
        back_image_data: payload.back_image_data || undefined
      })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data?.error?.message || 'Could not update business');
    return data.data || data;
  },

  // 25. Owner: Upload / replace business card image (one side)
  async uploadBusinessCardImage(id, side, imageData, token = '') {
    const headers = { 'Content-Type': 'application/json' };
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const res = await fetch(`${API_BASE_URL}/owner/businesses/${id}/card-image`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ side, image_data: imageData })
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data?.error?.message || 'Could not upload card image');
    return data.data || data;
  }
};
