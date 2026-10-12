<template>
  <section class="automation-form">
    <header class="columns page-header">
      <div class="column is-10">
        <h1 class="title is-4">
          {{ automation.name || 'Automation' }}
        </h1>
        <p class="is-size-7 has-text-grey">
          Only new signups from the website form or the signup gateway start this automation. Imports do not.
        </p>
      </div>
      <div class="column has-text-right">
        <router-link :to="{ name: 'automations' }">Back to automations</router-link>
      </div>
    </header>

    <b-loading :active="isLoading" :is-full-page="false" />

    <form v-if="loaded" @submit.prevent="onSave">
      <div class="box">
        <b-field label="Name" label-position="on-border">
          <b-input :maxlength="200" v-model="form.name" name="name" required :disabled="!canManage"
            data-cy="automation-name" />
        </b-field>

        <b-field label="List" label-position="on-border"
          :message="counts.enrolled > 0 ? 'The list cannot change after the first enrollment.' : ''">
          <b-select v-model="form.list_id" name="list_id" expanded required
            :disabled="!canManage || counts.enrolled > 0" data-cy="automation-list">
            <option v-for="l in listChoices" :key="l.id" :value="l.id">{{ l.name }}</option>
          </b-select>
        </b-field>

        <b-field>
          <b-switch v-model="form.enabled" :disabled="!canManage" data-cy="automation-enabled">
            Enabled
          </b-switch>
        </b-field>
      </div>

      <div class="box" v-for="(step, i) in form.steps" :key="step.position">
        <h5 class="title is-6">Step {{ step.position }}</h5>
        <div class="columns">
          <div class="column is-4">
            <b-field label="Delay in minutes" label-position="on-border"
              :message="step.delay_minutes === 0 ? 'Send immediately' : ''">
              <b-numberinput v-model="form.steps[i].delay_minutes" :min="0" :max="525600" :disabled="!canManage"
                controls-position="compact" data-cy="step-delay" />
            </b-field>
          </div>
          <div class="column">
            <p><strong>Subject:</strong> {{ stepInfo(step).subject }}</p>
            <p><strong>Messenger:</strong> {{ stepInfo(step).messenger }}</p>
            <p><strong>From:</strong> {{ stepInfo(step).from_email }}</p>
            <p data-cy="step-sent"><strong>Sent:</strong> {{ $utils.formatNumber(stepSent(step)) }}</p>
          </div>
        </div>
        <div>
          <router-link :to="{ name: 'campaign', params: { id: step.campaign_id } }" class="button is-small"
            data-cy="step-edit">
            Edit email
          </router-link>
          <router-link v-if="$can('campaigns:get_analytics')"
            :to="{ name: 'campaignReport', params: { id: step.campaign_id } }" class="button is-small ml-2"
            data-cy="step-stats">
            Stats
          </router-link>
        </div>
      </div>

      <div class="buttons">
        <b-button v-if="canManage" native-type="submit" type="is-primary" :loading="isSaving" data-cy="btn-save">
          {{ $t('globals.buttons.save') }}
        </b-button>
        <b-button v-if="canManage && !automation.enabled" type="is-danger is-light" icon-left="trash-can-outline"
          data-cy="btn-delete" @click="$utils.confirm(`Delete ${automation.name}?`, onDelete)">
          {{ $t('globals.buttons.delete') }}
        </b-button>
      </div>
    </form>
  </section>
</template>

<script>
// FORK(hcm): Automations edit page.
import Vue from 'vue';
import { mapState } from 'vuex';

export default Vue.extend({
  name: 'AutomationForm',

  data() {
    return {
      isLoading: false,
      isSaving: false,
      loaded: false,
      automation: {},
      serverSteps: [],
      counts: { enrolled: 0 },
      form: {
        name: '', list_id: null, enabled: false, steps: [],
      },
    };
  },

  methods: {
    load() {
      this.isLoading = true;
      return this.$api.getAutomation(this.$route.params.id).then((data) => {
        this.automation = data.automation;
        this.serverSteps = data.steps || [];
        this.counts = data.counts || { enrolled: 0 };
        this.form = {
          name: data.automation.name,
          list_id: data.automation.list_id,
          enabled: data.automation.enabled,
          steps: this.serverSteps.map((s) => ({
            position: s.position, delay_minutes: s.delay_minutes, campaign_id: s.campaign_id,
          })),
        };
        this.loaded = true;
      }).finally(() => { this.isLoading = false; });
    },

    stepInfo(step) {
      const s = this.serverSteps.find((i) => i.position === step.position);
      return (s && s.campaign) || {};
    },

    stepSent(step) {
      const s = this.serverSteps.find((i) => i.position === step.position);
      return s ? s.sent : 0;
    },

    onSave() {
      this.isSaving = true;
      this.$api.updateAutomation(this.automation.id, {
        name: this.form.name,
        list_id: this.form.list_id,
        enabled: this.form.enabled,
        steps: this.form.steps.map((s) => ({ position: s.position, delay_minutes: s.delay_minutes })),
      }).then(() => {
        this.$utils.toast(this.$t('globals.messages.updated', { name: this.form.name }));
      }).finally(() => {
        this.isSaving = false;
        // Reload so a refused change shows the true state.
        this.load();
      });
    },

    onDelete() {
      this.$api.deleteAutomation(this.automation.id).then(() => {
        this.$router.push({ name: 'automations' });
      });
    },
  },

  computed: {
    ...mapState(['lists']),

    canManage() {
      return this.$can('campaigns:manage_all', 'campaigns:manage');
    },

    listChoices() {
      return (this.lists && this.lists.results) || [];
    },
  },

  mounted() {
    this.load();
  },
});
</script>
