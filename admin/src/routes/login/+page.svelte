<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';

	let email = $state('');
	let password = $state('');
	let submitting = $state(false);
	let errorMessage = $state('');

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		errorMessage = '';
		submitting = true;

		try {
			await auth.login(email, password);
			await goto('/');
		} catch {
			// The contract only defines one failure mode for login (401
			// invalid_credentials); collapse anything else (network errors,
			// unexpected statuses) into the same message rather than leaking
			// API/transport detail into the UI.
			errorMessage = 'Invalid email or password';
		} finally {
			submitting = false;
		}
	}
</script>

<div class="flex min-h-screen items-center justify-center bg-stone-50 px-4">
	<Card.Root class="w-full max-w-sm">
		<Card.Header>
			<h1 class="text-2xl font-semibold text-stone-900">PocketPress</h1>
			<Card.Description>Sign in to your writing dashboard.</Card.Description>
		</Card.Header>

		<Card.Content>
			<form class="space-y-4" onsubmit={handleSubmit}>
				<div class="space-y-1.5">
					<Label for="email">Email</Label>
					<Input
						id="email"
						name="email"
						type="email"
						autocomplete="email"
						placeholder="you@example.com"
						bind:value={email}
						required
					/>
				</div>

				<div class="space-y-1.5">
					<Label for="password">Password</Label>
					<Input
						id="password"
						name="password"
						type="password"
						autocomplete="current-password"
						placeholder="••••••••"
						bind:value={password}
						required
					/>
				</div>

				{#if errorMessage}
					<p class="text-sm text-destructive" role="alert">{errorMessage}</p>
				{/if}

				<Button type="submit" variant="primary" class="w-full" disabled={submitting}>
					{submitting ? 'Signing in…' : 'Sign in'}
				</Button>
			</form>
		</Card.Content>
	</Card.Root>
</div>
