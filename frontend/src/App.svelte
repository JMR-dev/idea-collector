<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type AuthInfo, type SubmitResult } from './api';

  let booting = $state(true);
  let info = $state<AuthInfo | null>(null);

  // Code entry
  let code = $state('');
  let authing = $state(false);
  let authError = $state('');

  // Submission
  let title = $state('');
  let body = $state('');
  let submitting = $state(false);
  let submitError = $state('');
  let result = $state<SubmitResult | null>(null);

  onMount(async () => {
    try {
      info = await api.session();
    } catch {
      info = null;
    } finally {
      booting = false;
    }
  });

  async function signIn(event: SubmitEvent) {
    event.preventDefault();
    if (!code.trim() || authing) return;
    authing = true;
    authError = '';
    try {
      info = await api.auth(code);
      code = '';
    } catch (e) {
      authError = e instanceof Error ? e.message : 'Could not check that code.';
    } finally {
      authing = false;
    }
  }

  async function submitIdea(event: SubmitEvent) {
    event.preventDefault();
    if (!title.trim() || !body.trim() || submitting) return;
    submitting = true;
    submitError = '';
    try {
      result = await api.submit(title, body);
      title = '';
      body = '';
    } catch (e) {
      submitError = e instanceof Error ? e.message : 'Could not submit your idea.';
    } finally {
      submitting = false;
    }
  }

  function submitAnother() {
    result = null;
    submitError = '';
  }

  async function signOut() {
    try {
      await api.logout();
    } finally {
      info = null;
      result = null;
    }
  }
</script>

<main>
  <div class="card">
    {#if booting}
      <p class="muted">Loading…</p>
    {:else if !info}
      <h1>Share an idea</h1>
      <p class="muted">Enter the invite code you were given to get started.</p>
      <form onsubmit={signIn}>
        <label for="code">Invite code</label>
        <input
          id="code"
          bind:value={code}
          placeholder="XXXX-XXXX-XXXX"
          autocomplete="one-time-code"
          autocapitalize="characters"
          spellcheck="false"
          disabled={authing}
        />
        {#if authError}<p class="error">{authError}</p>{/if}
        <button type="submit" disabled={authing || !code.trim()}>
          {authing ? 'Checking…' : 'Continue'}
        </button>
      </form>
    {:else if result}
      <h1>Thank you! 🎉</h1>
      <p>Your idea for <strong>{info.project_name}</strong> has been received.</p>
      <p class="muted">It was logged as item #{result.issue_number}.</p>
      <button type="button" onclick={submitAnother}>Submit another idea</button>
      <button type="button" class="link" onclick={signOut}>Sign out</button>
    {:else}
      <div class="topbar">
        <span class="muted">Hi {info.display_name}</span>
        <button type="button" class="link" onclick={signOut}>Sign out</button>
      </div>
      <h1>{info.project_name}</h1>
      <p class="muted">What would you like to suggest?</p>
      <form onsubmit={submitIdea}>
        <label for="title">Title</label>
        <input
          id="title"
          bind:value={title}
          maxlength="200"
          placeholder="A short summary"
          disabled={submitting}
        />
        <label for="body">Description</label>
        <textarea
          id="body"
          bind:value={body}
          rows="6"
          maxlength="5000"
          placeholder="Describe your idea in a little more detail…"
          disabled={submitting}
        ></textarea>
        {#if submitError}<p class="error">{submitError}</p>{/if}
        <button type="submit" disabled={submitting || !title.trim() || !body.trim()}>
          {submitting ? 'Sending…' : 'Send idea'}
        </button>
      </form>
    {/if}
  </div>
  <footer class="muted">Your name is shared with the team along with your idea.</footer>
</main>
