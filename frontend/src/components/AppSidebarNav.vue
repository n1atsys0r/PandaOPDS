<script setup>
import {
  Package2Icon as Package2,
  LayoutDashboardIcon as LayoutDashboard,
  PanelsTopLeftIcon as PanelsTopLeft,
  CompassIcon as Compass,
  SettingsIcon as Settings,
} from "@lucide/vue";
import {
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";
import { t } from "@/i18n.js";

const props = defineProps({ page: { type: String, required: true } });
const emit = defineEmits(["navigate"]);
const { setOpenMobile } = useSidebar();

function navigate(page) {
  emit("navigate", page);
  setOpenMobile(false);
}
</script>

<template>
  <SidebarContent>
    <!-- 主导航功能区 -->
    <SidebarGroup>
      <SidebarGroupContent>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'dashboard'"
              :tooltip="t('nav.dashboard')"
              @click="navigate('dashboard')"
            >
              <LayoutDashboard /><span class="group-data-[collapsible=icon]:hidden">{{ t("nav.dashboard") }}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'home'"
              :tooltip="t('explore.edit_home')"
              @click="navigate('home')"
            >
              <PanelsTopLeft /><span class="group-data-[collapsible=icon]:hidden">{{ t("explore.edit_home") }}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'explore'"
              :tooltip="t('nav.explore')"
              @click="navigate('explore')"
            >
              <Compass /><span class="group-data-[collapsible=icon]:hidden">{{ t("nav.explore") }}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'archive'"
              :tooltip="t('nav.archive')"
              @click="navigate('archive')"
            >
              <Package2 /><span class="group-data-[collapsible=icon]:hidden">{{ t("nav.archive") }}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>

    <!-- 置底工具区 -->
    <SidebarGroup class="mt-auto">
      <SidebarGroupContent>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              :is-active="props.page === 'config'"
              :tooltip="t('nav.settings')"
              @click="navigate('config')"
            >
              <Settings /><span class="group-data-[collapsible=icon]:hidden">{{ t("nav.settings") }}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  </SidebarContent>
</template>
