<script setup>
import { ref } from "vue";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { CircleAlertIcon as CircleAlert, RefreshCwIcon as RefreshCw } from "@lucide/vue";
import { useApp } from "@/state.js";
import { t } from "@/i18n.js";

const s = useApp();
const username = ref("");
const password = ref("");
const errorMessage = ref("");
const loading = ref(false);

async function handleLogin() {
  if (!username.value.trim() || !password.value) {
    errorMessage.value = t("auth.error_empty");
    return;
  }
  loading.value = true;
  errorMessage.value = "";
  try {
    const ok = await s.login(username.value.trim(), password.value);
    if (!ok) {
      errorMessage.value = s.loginError?.value || t("auth.error_credentials");
    }
  } catch (err) {
    errorMessage.value = err.message || t("auth.error_failed");
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <Card class="rounded-xl border border-border bg-card text-card-foreground shadow-sm py-0 gap-0">
      <CardHeader class="p-6 pb-4">
        <CardTitle class="text-2xl font-bold tracking-tight text-foreground">
          {{ t("auth.title") }}
        </CardTitle>
      </CardHeader>
      <CardContent class="p-6 pt-0">
        <form @submit.prevent="handleLogin">
          <div class="flex flex-col gap-6">
            <Alert v-if="errorMessage" variant="destructive" class="py-2.5 px-3">
              <CircleAlert class="size-4 shrink-0 text-destructive" />
              <AlertDescription class="text-xs">{{ errorMessage }}</AlertDescription>
            </Alert>

            <div class="grid gap-2">
              <label for="username" class="text-sm font-medium leading-none text-foreground select-none">
                {{ t("auth.username") }}
              </label>
              <Input
                id="username"
                v-model="username"
                type="text"
                autocomplete="username"
                required
                :disabled="loading"
                class="h-9"
              />
            </div>

            <div class="grid gap-2">
              <label for="password" class="text-sm font-medium leading-none text-foreground select-none">
                {{ t("auth.password") }}
              </label>
              <Input
                id="password"
                v-model="password"
                type="password"
                autocomplete="current-password"
                required
                :disabled="loading"
                class="h-9"
              />
            </div>

            <Button
              type="submit"
              class="w-full h-9 font-medium text-sm shadow-xs"
              :disabled="loading"
            >
              <RefreshCw v-if="loading" class="size-3.5 mr-2 animate-spin" />
              {{ loading ? t("auth.signing_in") : t("auth.submit") }}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  </div>
</template>
