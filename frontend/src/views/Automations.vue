<template>
  <section class="automations">
    <header class="columns page-header">
      <div class="column is-10">
        <h1 class="title is-4">
          Automations
          <span v-if="automations.length">({{ automations.length }})</span>
        </h1>
        <p class="is-size-7 has-text-grey">
          Only new signups from the website form or the signup gateway start an automation. Imports do not.
        </p>
      </div>
      <div class="column has-text-right">
        <b-field v-if="$can('campaigns:manage_all', 'campaigns:manage')" expanded>
          <b-button expanded type="is-primary" icon-left="plus" class="btn-new" @click="showNewForm"
            data-cy="btn-new">
            {{ $t('globals.buttons.new') }}
          </b-button>
        </b-field>
      </div>
    </header>

    <b-table :data="automations" :loading="loading.campaigns" hoverable default-sort="name">
      <b-table-column v-slot="props" field="name" :label="$t('globals.fields.name')" sortable>
        <router-link :to="{ name: 'automation', params: { id: props.row.id } }" data-cy="automation-name">
          {{ props.row.name }}
        </router-link>
      </b-table-column>

      <b-table-column v-slot="props" field="list_name" :label="$t('globals.terms.list')" sortable>
        {{ props.row.list_name }}
        <b-tag v-if="props.row.brand" class="is-small is-info is-light" data-cy="brand-label">
          {{ brandName(props.row.brand) }}
        </b-tag>
      </b-table-column>

      <b-table-column v-slot="props" field="enabled" label="Enabled" sortable>
        <b-switch :value="props.row.enabled" :disabled="!$can('campaigns:manage_all', 'campaigns:manage')"
          data-cy="automation-enabled" @input="(v) => onToggle(props.row, v)" />
      </b-table-column>

      <b-table-column v-slot="props" field="enrolled" label="Enrolled" numeric sortable>
        {{ $utils.formatNumber(props.row.enrolled) }}
      </b-table-column>

      <b-table-column v-slot="props" field="sent" label="Sent" numeric sortable>
        {{ $utils.formatNumber(props.row.sent) }}
      </b-table-column>

      <b-table-column v-slot="props" field="failed" label="Failed" numeric sortable>
        {{ $utils.formatNumber(props.row.failed) }}
      </b-table-column>

      <b-table-column v-slot="props" field="unknown" label="Unknown" numeric sortable>
        {{ $utils.formatNumber(props.row.unknown) }}
      </b-table-column>

      <template #empty v-if="!loading.campaigns">
        <empty-placeholder />
      </template>
    </b-table>

    <b-modal scroll="keep" :aria-modal="true" :active.sync="isFormVisible" :width="500">
      <form @submit.prevent="onCreate">
        <div class="modal-card content" style="width: auto">
          <header class="modal-card-head">
            <h4>New automation</h4>
          </header>
          <section expanded class="modal-card-body">
            <b-field :label="$t('globals.fields.name')" label-position="on-border">
              <b-input :maxlength="200" v-model="form.name" name="name" required data-cy="automation-form-name" />
            </b-field>
            <b-field :label="$t('globals.terms.list')" label-position="on-border">
              <b-select v-model="form.list_id" name="list_id" expanded required data-cy="automation-form-list">
                <option v-for="l in listChoices" :key="l.id" :value="l.id">{{ l.name }}</option>
              </b-select>
            </b-field>
          </section>
          <footer class="modal-card-foot has-text-right">
            <b-button @click="isFormVisible = false">
              {{ $t('globals.buttons.close') }}
            </b-button>
            <b-button native-type="submit" type="is-primary" :loading="isSaving" data-cy="btn-save">
              {{ $t('globals.buttons.save') }}
            </b-button>
          </footer>
        </div>
      </form>
    </b-modal>
  </section>
</template>

<script>
// FORK(hcm): Automations list page. The brand scope comes from the X-Listmonk-Brand
// header; a brand switch reloads the tab, so the page needs no brand watcher.
import Vue from 'vue';
import { mapState } from 'vuex';
import EmptyPlaceholder from '../components/EmptyPlaceholder.vue';

export default Vue.extend({
  name: 'Automations',

  components: { EmptyPlaceholder },

  data() {
    return {
      automations: [],
      isFormVisible: false,
      isSaving: false,
      form: { name: '', list_id: null },
    };
  },

  methods: {
    getAutomations() {
      return this.$api.getAutomations().then((data) => {
        this.automations = data || [];
      });
    },

    brandName(slug) {
      const b = (this.brands || []).find((i) => i.slug === slug);
      return b ? b.name : slug;
    },

    showNewForm() {
      this.form = { name: '', list_id: null };
      this.isFormVisible = true;
    },

    onCreate() {
      this.isSaving = true;
      this.$api.createAutomation({ name: this.form.name, list_id: this.form.list_id }).then((data) => {
        this.isFormVisible = false;
        this.$router.push({ name: 'automation', params: { id: data.id } });
      }).finally(() => { this.isSaving = false; });
    },

    // The server replaces the whole automation, so send the stored steps with the new state.
    onToggle(row, enabled) {
      this.$api.getAutomation(row.id).then((data) => {
        const { automation, steps } = data;
        return this.$api.updateAutomation(row.id, {
          name: automation.name,
          list_id: automation.list_id,
          enabled,
          steps: steps.map((s) => ({ position: s.position, delay_minutes: s.delay_minutes })),
        });
      }).then(() => {
        this.$utils.toast(`${row.name}: ${enabled ? 'enabled' : 'disabled'}`);
      }).finally(() => {
        // On a refusal the API layer shows the server message. Reload to show the true state.
        this.getAutomations();
      });
    },
  },

  computed: {
    ...mapState(['loading', 'lists', 'brands']),

    listChoices() {
      return (this.lists && this.lists.results) || [];
    },
  },

  mounted() {
    this.getAutomations();
  },
});
</script>
