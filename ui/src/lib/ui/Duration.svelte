<script lang="ts">import { duration } from '../format';let { ms = 0 }: { ms?: number } = $props();</script><span>{duration(ms)}</span>
