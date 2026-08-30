/**
 * Thin fetch wrapper that handles 401 redirects.
 * Cookies are sent automatically by the browser — no token injection needed.
 */
export async function authFetch(
  input: RequestInfo | URL,
  init?: RequestInit
): Promise<Response> {
  const response = await fetch(input, init);

  if (response.status === 401) {
    // Clear the JS-readable indicator cookie
    document.cookie = "qm_logged_in=; Path=/; Max-Age=0";
    if (!window.location.pathname.startsWith("/login")) {
      window.location.href = "/login/";
    }
  }

  return response;
}
