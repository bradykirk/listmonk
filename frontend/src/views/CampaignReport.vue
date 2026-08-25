<template>
  <section class="analytics campaign-report content relative">
    <header class="columns page-header">
      <div class="column is-8">
        <p v-if="campaign.status" class="tags">
          <b-tag :class="campaign.status">
            {{ $t(`campaigns.status.${campaign.status}`) }}
          </b-tag>
          <span class="has-text-grey-light is-size-7">
            {{ $t('globals.fields.id') }}: {{ campaign.id }}
          </span>
        </p>
        <h1 class="title is-4">{{ campaign.name }}</h1>
        <p v-if="campaign.startedAt" class="has-text-grey">
          {{ $t('campaigns.startedAt') }}: {{ $utils.niceDate(campaign.startedAt, true) }}
          <template v-if="campaign.lists">
            &mdash;
            <span v-for="(l, i) in campaign.lists" :key="l.id">{{ i > 0 ? ', ' : '' }}{{ l.name }}</span>
          </template>
        </p>
      </div>
      <div class="column is-4 has-text-right">
        <router-link :to="{ name: 'campaign', params: { id: $route.params.id } }" class="is-size-7">
          {{ $tc('globals.terms.campaign', 1) }}
        </router-link>
        &nbsp;&middot;&nbsp;
        <router-link :to="{ name: 'campaignAnalytics', query: { id: $route.params.id } }" class="is-size-7">
          {{ $t('analytics.comparePage') }}
        </router-link>
      </div>
    </header>

    <div v-if="serverConfig.privacy.disable_tracking" class="notification is-info">
      {{ $t('analytics.trackingDisabled') }}
    </div>
    <div v-else-if="!serverConfig.privacy.individual_tracking" class="notification is-info">
      {{ $t('analytics.ratesNeedTracking') }}
    </div>
    <div v-else-if="isPreTracking" class="notification is-info">
      {{ $t('analytics.preTrackingNote') }}
    </div>

    <!-- summary tiles -->
    <div v-if="summary" class="columns is-multiline tiles mt-2">
      <div class="column">
        <div class="box tile-box">
          <label for="#">{{ $t('campaigns.sent') }}</label>
          <h3 class="title is-4">{{ $utils.formatNumber(summary.sent) }}</h3>
          <p class="is-size-7 has-text-grey">
            {{ $utils.formatNumber(summary.delivered) }} {{ $t('analytics.delivered').toLowerCase() }}
            &middot; {{ $utils.formatNumber(summary.bounced) }} {{ $t('analytics.bounced').toLowerCase() }}
          </p>
        </div>
      </div>

      <div class="column">
        <div class="box tile-box">
          <label for="#">
            {{ $t('analytics.opened') }}
            <b-tooltip :label="$t('analytics.opensApproxNote')" type="is-dark" multilined>
              <b-icon icon="information-outline" size="is-small" />
            </b-tooltip>
          </label>
          <h3 class="title is-4">
            <template v-if="summary.openRate !== null">{{ summary.openRate }}%</template>
            <template v-else>{{ $utils.formatNumber(summary.viewsTotal) }}</template>
          </h3>
          <p class="is-size-7 has-text-grey">
            <template v-if="summary.openRate !== null">
              {{ $utils.formatNumber(summary.viewsUnique) }} {{ $t('analytics.unique').toLowerCase() }}
              &middot; {{ $utils.formatNumber(summary.viewsTotal) }} {{ $t('analytics.total').toLowerCase() }}
            </template>
            <template v-else>{{ $t('analytics.total').toLowerCase() }}</template>
          </p>
          <b-progress v-if="summary.openRate !== null" :value="summary.openRate" size="is-small" type="is-primary" />
        </div>
      </div>

      <div class="column">
        <div class="box tile-box">
          <label for="#">{{ $t('analytics.clicked') }}</label>
          <h3 class="title is-4">
            <template v-if="summary.clickRate !== null">{{ summary.clickRate }}%</template>
            <template v-else>{{ $utils.formatNumber(summary.clicksTotal) }}</template>
          </h3>
          <p class="is-size-7 has-text-grey">
            <template v-if="summary.clickRate !== null">
              {{ $utils.formatNumber(summary.clicksUnique) }} {{ $t('analytics.unique').toLowerCase() }}
              <template v-if="summary.clickToOpenRate !== null">
                &middot; {{ $t('analytics.pctOfOpeners', { pct: summary.clickToOpenRate }) }}
              </template>
            </template>
            <template v-else>{{ $t('analytics.total').toLowerCase() }}</template>
          </p>
          <b-progress v-if="summary.clickRate !== null" :value="summary.clickRate" size="is-small" type="is-primary" />
        </div>
      </div>

      <div class="column">
        <div class="box tile-box">
          <label for="#">{{ $t('analytics.bounced') }}</label>
          <h3 class="title is-4">{{ summary.bounceRate }}%</h3>
          <p class="is-size-7 has-text-grey">
            <router-link :to="{ name: 'bounces', query: { campaign_id: campaign.id } }">
              {{ $utils.formatNumber(summary.bounced) }}
            </router-link>
            &middot; {{ $utils.formatNumber(summary.bouncedHard) }} {{ $t('analytics.bouncedHard').toLowerCase() }},
            {{ $utils.formatNumber(summary.bouncedSoft) }} {{ $t('analytics.bouncedSoft').toLowerCase() }}
          </p>
          <b-progress :value="summary.bounceRate" size="is-small" type="is-danger" />
        </div>
      </div>

      <div class="column">
        <div class="box tile-box">
          <label for="#">{{ $t('analytics.unsubscribed') }}</label>
          <h3 class="title is-4">{{ summary.unsubRate }}%</h3>
          <p class="is-size-7 has-text-grey">
            {{ $utils.formatNumber(summary.unsubs) }}
            <template v-if="summary.complaints > 0">
              &middot; {{ $utils.formatNumber(summary.complaints) }} {{ $t('analytics.complaints').toLowerCase() }}
            </template>
          </p>
          <b-progress :value="summary.unsubRate" size="is-small" type="is-warning" />
        </div>
      </div>
    </div><!-- tiles -->

    <!-- first 24 hours -->
    <section v-if="campaign.startedAt" class="charts mt-6">
      <h4>{{ $t('analytics.first24h') }}</h4>
      <div class="chart">
        <b-loading v-if="chartLoading" :active="chartLoading" :is-full-page="false" />
        <chart v-else-if="chartData" type="line" :data="chartData" />
      </div>
    </section>

    <!-- link activity -->
    <section class="mt-6">
      <h4>{{ $t('analytics.linkActivity') }}</h4>
      <b-table :data="links" hoverable default-sort="total" default-sort-direction="desc" paginated per-page="10"
        :pagination-simple="true">
        <b-table-column v-slot="props" field="url" :label="$t('analytics.links')" sortable>
          <a :href="props.row.url" target="_blank" rel="noopener noreferrer" class="link-url">
            {{ props.row.url }}
          </a>
        </b-table-column>
        <b-table-column v-slot="props" field="unique" :label="$t('analytics.uniqueClicks')" numeric sortable>
          {{ $utils.formatNumber(props.row.unique) }}
        </b-table-column>
        <b-table-column v-slot="props" field="total" :label="$t('analytics.totalClicks')" numeric sortable>
          {{ $utils.formatNumber(props.row.total) }}
        </b-table-column>
        <b-table-column v-slot="props" field="pct" :label="$t('analytics.pctOfClickers')" numeric>
          <template v-if="summary && summary.clicksUnique > 0">
            {{ ((props.row.unique / summary.clicksUnique) * 100).toFixed(1) }}%
          </template>
          <template v-else>&mdash;</template>
        </b-table-column>
        <template #empty>
          <empty-placeholder />
        </template>
      </b-table>
    </section>

    <!-- subscriber activity -->
    <section v-if="serverConfig.privacy.individual_tracking" class="mt-6">
      <h4>{{ $t('analytics.subscriberActivity') }}</h4>
      <b-tabs v-model="activityTab" @input="onActivityTab">
        <b-tab-item :label="$t('analytics.opened')" value="viewed" />
        <b-tab-item :label="$t('analytics.clicked')" value="clicked" />
        <b-tab-item :label="$t('analytics.didntOpen')" value="not_viewed" />
        <b-tab-item :label="$t('analytics.unsubscribed')" value="unsubscribed" />
      </b-tabs>
      <p v-if="activityTab === 'not_viewed'" class="is-size-7 has-text-grey">
        {{ $t('analytics.nonViewersApproxNote') }}
      </p>
      <b-table :data="activity.results" hoverable paginated backend-pagination :total="activity.total"
        :per-page="activity.perPage" :current-page="activity.page" @page-change="onActivityPage"
        :loading="loading.campaigns">
        <b-table-column v-slot="props" field="email" :label="$t('subscribers.email')">
          {{ props.row.email }}
        </b-table-column>
        <b-table-column v-slot="props" field="name" :label="$t('globals.fields.name')">
          {{ props.row.name }}
        </b-table-column>
        <b-table-column v-slot="props" field="first_at" :label="$t('analytics.firstSeen')">
          <template v-if="props.row.firstAt">
            {{ $utils.niceDate(props.row.firstAt, true) }}
          </template>
          <template v-else>&mdash;</template>
        </b-table-column>
        <b-table-column v-slot="props" field="count" :label="$t('analytics.count')" numeric>
          <template v-if="activityTab !== 'not_viewed'">{{ $utils.formatNumber(props.row.count) }}</template>
          <template v-else>&mdash;</template>
        </b-table-column>
        <template #empty>
          <empty-placeholder />
        </template>
      </b-table>
    </section>
  </section>
</template>

<script>
import Vue from 'vue';
import dayjs from 'dayjs';
import { mapState } from 'vuex';
import { colors } from '../constants';
import Chart from '../components/Chart.vue';
import EmptyPlaceholder from '../components/EmptyPlaceholder.vue';

export default Vue.extend({
  components: {
    Chart,
    EmptyPlaceholder,
  },

  data() {
    return {
      campaign: {},
      summary: null,
      links: [],
      chartData: null,
      chartLoading: false,
      activityTab: 'viewed',
      activity: {
        results: [], total: 0, page: 1, perPage: 20,
      },
    };
  },

  methods: {
    getData() {
      const { id } = this.$route.params;

      this.$api.getCampaign(id).then((camp) => {
        this.campaign = camp;
        if (camp.startedAt) {
          this.getChart(camp);
        }
      });

      this.$api.getCampaignAnalyticsSummary(id).then((data) => {
        this.summary = data;
      });

      this.$api.getCampaignLinkStats(id).then((data) => {
        this.links = data;
      });

      if (this.serverConfig.privacy.individual_tracking) {
        this.getActivity(1);
      }
    },

    getChart(camp) {
      const { id } = this.$route.params;
      const from = dayjs(camp.startedAt).toISOString();
      const to = dayjs(camp.startedAt).add(24, 'hour').toISOString();

      this.chartLoading = true;
      Promise.all([
        this.$api.getCampaignViewCounts({ id: [id], from, to }),
        this.$api.getCampaignClickCounts({ id: [id], from, to }),
      ]).then(([views, clicks]) => {
        // Merged, sorted category labels: without them, Chart.js appends
        // click-only time buckets after all view buckets, out of order.
        const labels = [...new Set(
          [...views, ...clicks].map((item) => this.formatDateTime(item.timestamp)),
        )].sort();
        this.chartData = {
          labels,
          datasets: [
            {
              label: this.$t('analytics.opened'),
              data: views.map((item) => ({ x: this.formatDateTime(item.timestamp), y: item.count })),
              borderColor: colors.primary,
              borderWidth: 2,
              pointHoverBorderWidth: 5,
              pointBorderWidth: 0.5,
            },
            {
              label: this.$t('analytics.clicked'),
              data: clicks.map((item) => ({ x: this.formatDateTime(item.timestamp), y: item.count })),
              borderColor: '#FFB50D',
              borderWidth: 2,
              pointHoverBorderWidth: 5,
              pointBorderWidth: 0.5,
            },
          ],
        };
        this.chartLoading = false;
      }).catch(() => {
        this.chartLoading = false;
      });
    },

    getActivity(page) {
      const { id } = this.$route.params;
      const tab = this.activityTab;
      this.$api.getCampaignSubscriberActivity(id, {
        type: tab, page, per_page: this.activity.perPage,
      }).then((data) => {
        // Discard responses that arrive after the user switched tabs.
        if (this.activityTab === tab) {
          this.activity = data;
        }
      });
    },

    onActivityTab() {
      this.getActivity(1);
    },

    onActivityPage(page) {
      this.getActivity(page);
    },

    formatDateTime(s) {
      return dayjs(s).format('YYYY-MM-DD HH:mm');
    },
  },

  computed: {
    ...mapState(['serverConfig', 'loading']),

    // True when the campaign has recorded events but no per-subscriber events:
    // it was sent before individual tracking was enabled, so rates are
    // unavailable and the tiles fall back to total counts.
    isPreTracking() {
      const { summary } = this;
      return summary !== null && summary.individualTracking && summary.openRate === null
        && (summary.viewsTotal > 0 || summary.clicksTotal > 0);
    },
  },

  mounted() {
    this.getData();
  },
});
</script>

<style lang="scss" scoped>
.tiles .tile-box {
  height: 100%;

  label {
    text-transform: uppercase;
    font-size: 0.7rem;
    letter-spacing: 0.05em;
    color: #888;
  }

  h3 {
    margin: 0.25rem 0;
  }
}

.link-url {
  word-break: break-all;
}

.chart {
  position: relative;
  min-height: 250px;
}
</style>
