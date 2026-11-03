<script lang="ts">
	/**
	 * Owner-only user management (plan section 9 routes: /settings/users).
	 * The server enforces owner-only access on every /api/admin/v1/users
	 * call; this page mirrors that client-side so editors get a friendly
	 * "no access" state instead of a wall of failed requests.
	 */
	import * as Card from '$lib/components/ui/card/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { auth } from '$lib/auth.svelte';
	import { ApiError, createUser, listUsers, updateUser, type User, type UserRole } from '$lib/api';

	type LoadState = 'pending' | 'success' | 'error';

	const isOwner = $derived(auth.user?.role === 'owner');

	let users = $state<User[]>([]);
	let loadState = $state<LoadState>('pending');
	let loadError = $state('');

	let newName = $state('');
	let newEmail = $state('');
	let newPassword = $state('');
	let newRole = $state<UserRole>('editor');
	let creating = $state(false);
	let createError = $state('');
	let createFieldErrors = $state<Record<string, string>>({});

	let rowBusy = $state<Record<string, boolean>>({});
	let rowErrors = $state<Record<string, string>>({});

	async function loadUsers() {
		loadState = 'pending';
		try {
			const { users: fetched } = await listUsers();
			users = fetched;
			loadState = 'success';
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Unable to load users.';
			loadState = 'error';
		}
	}

	$effect(() => {
		if (isOwner) {
			loadUsers();
		}
	});

	async function handleCreate(event: SubmitEvent) {
		event.preventDefault();
		createError = '';
		createFieldErrors = {};
		creating = true;

		try {
			const { user } = await createUser({
				name: newName,
				email: newEmail,
				password: newPassword,
				role: newRole
			});
			users = [...users, user];
			newName = '';
			newEmail = '';
			newPassword = '';
			newRole = 'editor';
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed') {
				createFieldErrors = err.details ?? {};
				createError = err.message;
			} else if (err instanceof ApiError) {
				createError = err.message;
			} else {
				createError = 'Unable to create user.';
			}
		} finally {
			creating = false;
		}
	}

	function describeUpdateError(err: unknown): string {
		if (err instanceof ApiError && err.code === 'last_owner') {
			return "This is the last owner — promote another user to owner first.";
		}
		if (err instanceof ApiError) return err.message;
		return err instanceof Error ? err.message : 'Unable to update user.';
	}

	async function toggleActive(user: User) {
		rowErrors = { ...rowErrors, [user.id]: '' };
		rowBusy = { ...rowBusy, [user.id]: true };
		try {
			const { user: updated } = await updateUser(user.id, { active: !user.active });
			users = users.map((u) => (u.id === updated.id ? updated : u));
		} catch (err) {
			rowErrors = { ...rowErrors, [user.id]: describeUpdateError(err) };
		} finally {
			rowBusy = { ...rowBusy, [user.id]: false };
		}
	}

	async function changeRole(user: User, role: UserRole) {
		if (role === user.role) return;
		rowErrors = { ...rowErrors, [user.id]: '' };
		rowBusy = { ...rowBusy, [user.id]: true };
		try {
			const { user: updated } = await updateUser(user.id, { role });
			users = users.map((u) => (u.id === updated.id ? updated : u));
		} catch (err) {
			rowErrors = { ...rowErrors, [user.id]: describeUpdateError(err) };
		} finally {
			rowBusy = { ...rowBusy, [user.id]: false };
		}
	}
</script>

<header class="mb-8 pt-2">
	<h1 class="text-[32px] font-medium leading-9 text-stone-900">Users</h1>
	<p class="mt-2 text-[13px] text-stone-500">Manage who can sign in to this site.</p>
</header>

{#if !isOwner}
			<Card.Root class="mt-8">
				<Card.Content class="py-10 text-center text-sm text-stone-500">
					You don't have access to this page. Ask an owner for access.
				</Card.Content>
			</Card.Root>
		{:else}
			<Card.Root class="mt-8">
				<Card.Header>
					<Card.Title class="text-sm font-medium text-stone-700">All users</Card.Title>
				</Card.Header>
				<Card.Content>
					{#if loadState === 'pending'}
						<p class="text-sm text-stone-400">Loading users…</p>
					{:else if loadState === 'error'}
						<p class="text-sm text-destructive">{loadError}</p>
					{:else}
						<div class="overflow-x-auto">
							<table class="w-full text-left text-sm">
								<thead>
									<tr class="border-b border-stone-200 text-stone-500">
										<th class="py-2 pr-4 font-medium">Name</th>
										<th class="py-2 pr-4 font-medium">Email</th>
										<th class="py-2 pr-4 font-medium">Role</th>
										<th class="py-2 pr-4 font-medium">Active</th>
										<th class="py-2 pr-4 font-medium">Last login</th>
										<th class="py-2 pr-4 font-medium">Actions</th>
									</tr>
								</thead>
								<tbody>
									{#each users as user (user.id)}
										<tr class="border-b border-stone-100 align-top">
											<td class="py-2 pr-4 text-stone-900">{user.name}</td>
											<td class="py-2 pr-4 text-stone-600">{user.email}</td>
											<td class="py-2 pr-4">
												<select
													class="border-input rounded-md border bg-transparent px-2 py-1 text-sm"
													value={user.role}
													disabled={rowBusy[user.id]}
													onchange={(event) =>
														changeRole(user, event.currentTarget.value as UserRole)}
												>
													<option value="owner">Owner</option>
													<option value="editor">Editor</option>
												</select>
											</td>
											<td class="py-2 pr-4">
												<Badge variant={user.active ? 'default' : 'secondary'}>
													{user.active ? 'Active' : 'Inactive'}
												</Badge>
											</td>
											<td class="py-2 pr-4 text-stone-500">
												{user.last_login_at ? new Date(user.last_login_at).toLocaleString() : 'Never'}
											</td>
											<td class="py-2 pr-4">
												<Button
													variant="outline"
													size="sm"
													disabled={rowBusy[user.id]}
													onclick={() => toggleActive(user)}
												>
													{user.active ? 'Deactivate' : 'Activate'}
												</Button>
											</td>
										</tr>
										{#if rowErrors[user.id]}
											<tr>
												<td colspan="6" class="pb-2 text-xs text-destructive">{rowErrors[user.id]}</td>
											</tr>
										{/if}
									{/each}
								</tbody>
							</table>
						</div>
					{/if}
				</Card.Content>
			</Card.Root>

			<Card.Root class="mt-6">
				<Card.Header>
					<Card.Title class="text-sm font-medium text-stone-700">New user</Card.Title>
				</Card.Header>
				<Card.Content>
					<form class="space-y-4" onsubmit={handleCreate} novalidate>
						<div class="grid gap-4 sm:grid-cols-2">
							<div class="space-y-1.5">
								<Label for="new-user-name">Name</Label>
								<Input id="new-user-name" bind:value={newName} aria-invalid={!!createFieldErrors.name} />
								{#if createFieldErrors.name}
									<p class="text-xs text-destructive">{createFieldErrors.name}</p>
								{/if}
							</div>

							<div class="space-y-1.5">
								<Label for="new-user-email">Email</Label>
								<Input
									id="new-user-email"
									type="email"
									bind:value={newEmail}
									aria-invalid={!!createFieldErrors.email}
								/>
								{#if createFieldErrors.email}
									<p class="text-xs text-destructive">{createFieldErrors.email}</p>
								{/if}
							</div>

							<div class="space-y-1.5">
								<Label for="new-user-password">Password</Label>
								<Input
									id="new-user-password"
									type="password"
									autocomplete="new-password"
									bind:value={newPassword}
									aria-invalid={!!createFieldErrors.password}
								/>
								{#if createFieldErrors.password}
									<p class="text-xs text-destructive">{createFieldErrors.password}</p>
								{/if}
							</div>

							<div class="space-y-1.5">
								<Label for="new-user-role">Role</Label>
								<select
									id="new-user-role"
									class="border-input h-9 w-full rounded-md border bg-transparent px-3 py-1 text-sm shadow-xs"
									bind:value={newRole}
								>
									<option value="editor">Editor</option>
									<option value="owner">Owner</option>
								</select>
							</div>
						</div>

						{#if createError}
							<p class="text-sm text-destructive" role="alert">{createError}</p>
						{/if}

						<Button type="submit" disabled={creating}>
							{creating ? 'Creating…' : 'New user'}
						</Button>
					</form>
				</Card.Content>
		</Card.Root>
	{/if}
