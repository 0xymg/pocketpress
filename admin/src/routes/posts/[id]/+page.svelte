<script lang="ts">
	import PostEditor from '$lib/components/posts/PostEditor.svelte';

	let { data }: { data: { id: string } } = $props();
</script>

<PostEditor postId={data.id} />
